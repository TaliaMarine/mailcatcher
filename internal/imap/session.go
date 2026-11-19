package imap

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net/mail"
	"net/textproto"
	"strings"
	"time"

	"github.com/TaliaMarine/mailcatcher/internal/credentials"
	"github.com/TaliaMarine/mailcatcher/internal/database"
	ws "github.com/TaliaMarine/mailcatcher/internal/websocket"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	gotextproto "github.com/emersion/go-message/textproto"
)

var errReadOnly = &imap.Error{Type: imap.StatusResponseTypeNo, Code: imap.ResponseCodeNoPerm, Text: "This server does not allow changing mailboxes"}

var errNoSuchMailbox = &imap.Error{Type: imap.StatusResponseTypeNo, Code: imap.ResponseCodeNonExistent, Text: "No such mailbox"}

var systemFlags = []imap.Flag{imap.FlagSeen, imap.FlagAnswered, imap.FlagFlagged, imap.FlagDeleted, imap.FlagDraft}

// selection is the mailbox a session has open. ids holds the message ids in
// ascending order; position+1 is the message's sequence number.
type selection struct {
	name      string
	mailboxID int64 // 0 for INBOX (all messages)
	readOnly  bool
	ids       []int64
}

type session struct {
	srv    *Server
	remote string
	user   string
	sel    *selection
	// flags are per-session: message id -> flags.
	flags map[int64]map[imap.Flag]struct{}
}

func (s *session) Close() error { return nil }

// -- Not authenticated --

func (s *session) Login(username, password string) error {
	if s.srv.limiter.blocked(s.remote) {
		s.srv.logger.Warn("IMAP login blocked: too many failures", "remote", s.remote)
		return imapserver.ErrAuthFailed
	}
	cred, err := s.srv.store.Authenticate(s.srv.ctx(), username, password)
	if errors.Is(err, credentials.ErrInvalidCredentials) {
		s.srv.limiter.fail(s.remote)
		s.srv.logger.Info("IMAP login failed", "remote", s.remote, "username", credentials.NormalizeUsername(username))
		return imapserver.ErrAuthFailed
	}
	if err != nil {
		s.srv.logger.Error("IMAP login error", "error", err)
		return &imap.Error{Type: imap.StatusResponseTypeNo, Code: imap.ResponseCodeUnavailable, Text: "Authentication backend unavailable"}
	}
	s.srv.limiter.reset(s.remote)
	s.user = cred.Username
	s.srv.logger.Info("IMAP login", "remote", s.remote, "username", cred.Username)
	return nil
}

// -- Folders --

func (s *session) resolve(name string) (id int64, canonical string, err error) {
	if strings.EqualFold(name, inbox) {
		return 0, inbox, nil
	}
	mb, err := s.srv.db.FindMailboxByAddress(s.srv.ctx(), name)
	if err != nil {
		return 0, "", err
	}
	if mb == nil {
		return 0, "", errNoSuchMailbox
	}
	return mb.ID, mb.Address, nil
}

func (s *session) selectData(ids []int64) (*imap.SelectData, error) {
	validity, err := s.srv.db.UIDValidity(s.srv.ctx())
	if err != nil {
		return nil, err
	}
	next, err := s.srv.db.NextMessageID(s.srv.ctx())
	if err != nil {
		return nil, err
	}
	return &imap.SelectData{
		Flags:          systemFlags,
		PermanentFlags: append(append([]imap.Flag{}, systemFlags...), imap.FlagWildcard),
		NumMessages:    uint32(len(ids)),
		UIDNext:        imap.UID(next),
		UIDValidity:    validity,
	}, nil
}

func (s *session) Select(name string, options *imap.SelectOptions) (*imap.SelectData, error) {
	id, canonical, err := s.resolve(name)
	if err != nil {
		return nil, err
	}
	ids, err := s.srv.db.ListMessageIDs(s.srv.ctx(), id)
	if err != nil {
		return nil, err
	}
	s.sel = &selection{name: canonical, mailboxID: id, readOnly: options != nil && options.ReadOnly, ids: ids}
	return s.selectData(ids)
}

func (s *session) Unselect() error {
	s.sel = nil
	return nil
}

func (s *session) Create(string, *imap.CreateOptions) error { return errReadOnly }
func (s *session) Delete(string) error                      { return errReadOnly }
func (s *session) Rename(string, string, *imap.RenameOptions) error {
	return errReadOnly
}

// Every folder is permanently subscribed; accept the commands so clients that
// subscribe on first sync do not report an error.
func (s *session) Subscribe(string) error   { return nil }
func (s *session) Unsubscribe(string) error { return nil }

func (s *session) Append(string, imap.LiteralReader, *imap.AppendOptions) (*imap.AppendData, error) {
	return nil, errReadOnly
}

func (s *session) Copy(imap.NumSet, string) (*imap.CopyData, error) { return nil, errReadOnly }

func (s *session) folderNames() ([]string, error) {
	boxes, err := s.srv.db.ListMailboxes(s.srv.ctx(), "address", "asc")
	if err != nil {
		return nil, err
	}
	names := []string{inbox}
	for _, b := range boxes {
		names = append(names, b.Address)
	}
	return names, nil
}

func (s *session) List(w *imapserver.ListWriter, ref string, patterns []string, options *imap.ListOptions) error {
	if len(patterns) == 0 { // LIST "" "": hierarchy delimiter query
		return w.WriteList(&imap.ListData{Attrs: []imap.MailboxAttr{imap.MailboxAttrNoSelect}, Delim: delim})
	}
	if options.SelectSpecialUse {
		return nil // no special-use folders
	}
	names, err := s.folderNames()
	if err != nil {
		return err
	}
	for _, name := range names {
		matched := false
		for _, p := range patterns {
			if imapserver.MatchList(name, delim, ref, p) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		data := &imap.ListData{Mailbox: name, Delim: delim, Attrs: []imap.MailboxAttr{imap.MailboxAttrSubscribed}}
		if options.ReturnStatus != nil {
			if data.Status, err = s.Status(name, options.ReturnStatus); err != nil {
				return err
			}
		}
		if err := w.WriteList(data); err != nil {
			return err
		}
	}
	return nil
}

func (s *session) Status(name string, options *imap.StatusOptions) (*imap.StatusData, error) {
	id, canonical, err := s.resolve(name)
	if err != nil {
		return nil, err
	}
	ids, err := s.srv.db.ListMessageIDs(s.srv.ctx(), id)
	if err != nil {
		return nil, err
	}
	sel, err := s.selectData(ids)
	if err != nil {
		return nil, err
	}

	data := &imap.StatusData{Mailbox: canonical}
	n := uint32(len(ids))
	if options.NumMessages {
		data.NumMessages = &n
	}
	if options.UIDNext {
		data.UIDNext = sel.UIDNext
	}
	if options.UIDValidity {
		data.UIDValidity = sel.UIDValidity
	}
	if options.NumUnseen {
		unseen := n
		for _, mid := range ids {
			if s.hasFlag(mid, imap.FlagSeen) {
				unseen--
			}
		}
		data.NumUnseen = &unseen
	}
	if options.NumDeleted {
		var deleted uint32
		data.NumDeleted = &deleted
	}
	if options.NumRecent {
		var recent uint32
		data.NumRecent = &recent
	}
	return data, nil
}

// -- Flags --

func (s *session) hasFlag(id int64, f imap.Flag) bool {
	_, ok := s.flags[id][canonical(f)]
	return ok
}

func canonical(f imap.Flag) imap.Flag { return imap.Flag(strings.ToLower(string(f))) }

func (s *session) flagList(id int64) []imap.Flag {
	var out []imap.Flag
	for _, f := range systemFlags {
		if s.hasFlag(id, f) {
			out = append(out, f)
		}
	}
	return out
}

// -- Selected state --

func (s *session) requireSelected() (*selection, error) {
	if s.sel == nil {
		return nil, &imap.Error{Type: imap.StatusResponseTypeBad, Text: "No mailbox selected"}
	}
	return s.sel, nil
}

// staticNumSet resolves "*" and returns the sequence numbers matched.
func (sel *selection) seqNums(numSet imap.NumSet) []uint32 {
	var out []uint32
	switch set := numSet.(type) {
	case imap.SeqSet:
		max := uint32(len(sel.ids))
		for i := range sel.ids {
			n := uint32(i) + 1
			if contains(set, n, max) {
				out = append(out, n)
			}
		}
	case imap.UIDSet:
		var max uint32
		if len(sel.ids) > 0 {
			max = uint32(sel.ids[len(sel.ids)-1])
		}
		for i, id := range sel.ids {
			if containsUID(set, uint32(id), max) {
				out = append(out, uint32(i)+1)
			}
		}
	}
	return out
}

func contains(set imap.SeqSet, n, max uint32) bool {
	for _, r := range set {
		lo, hi := r.Start, r.Stop
		if lo == 0 {
			lo = max
		}
		if hi == 0 {
			hi = max
		}
		if lo > hi {
			lo, hi = hi, lo
		}
		if n >= lo && n <= hi {
			return true
		}
	}
	return false
}

func containsUID(set imap.UIDSet, n, max uint32) bool {
	for _, r := range set {
		lo, hi := uint32(r.Start), uint32(r.Stop)
		if lo == 0 {
			lo = max
		}
		if hi == 0 {
			hi = max
		}
		if lo > hi {
			lo, hi = hi, lo
		}
		if n >= lo && n <= hi {
			return true
		}
	}
	return false
}

func (s *session) Fetch(w *imapserver.FetchWriter, numSet imap.NumSet, options *imap.FetchOptions) error {
	sel, err := s.requireSelected()
	if err != nil {
		return err
	}
	needRaw := options.Envelope || options.BodyStructure != nil ||
		len(options.BodySection) > 0 || len(options.BinarySection) > 0 || len(options.BinarySectionSize) > 0

	markSeen := !sel.readOnly
	if markSeen {
		markSeen = false
		for _, bs := range options.BodySection {
			if !bs.Peek {
				markSeen = true
			}
		}
	}

	for _, seq := range sel.seqNums(numSet) {
		id := sel.ids[seq-1]
		msg, err := s.srv.db.GetIMAPMessage(s.srv.ctx(), id, needRaw)
		if err != nil {
			return err
		}
		if msg == nil {
			continue // deleted since the client last synced
		}
		if markSeen {
			s.setFlags(id, &imap.StoreFlags{Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagSeen}})
			options = withFlags(options)
		}
		if err := writeMessage(w.CreateMessage(seq), msg, s.flagList(id), options); err != nil {
			return err
		}
	}
	return nil
}

// withFlags returns options that also report flags (set after an implicit \Seen).
func withFlags(o *imap.FetchOptions) *imap.FetchOptions {
	c := *o
	c.Flags = true
	return &c
}

func writeMessage(w *imapserver.FetchResponseWriter, msg *database.IMAPMessage, flags []imap.Flag, o *imap.FetchOptions) error {
	w.WriteUID(imap.UID(msg.ID))
	if o.Flags {
		w.WriteFlags(flags)
	}
	if o.InternalDate {
		w.WriteInternalDate(msg.CreatedAt)
	}
	if o.RFC822Size {
		w.WriteRFC822Size(int64(msg.Size))
	}
	if o.Envelope {
		if h, err := gotextproto.ReadHeader(bufio.NewReader(bytes.NewReader(msg.Raw))); err == nil {
			w.WriteEnvelope(imapserver.ExtractEnvelope(h))
		}
	}
	if o.BodyStructure != nil {
		w.WriteBodyStructure(imapserver.ExtractBodyStructure(bytes.NewReader(msg.Raw)))
	}
	for _, bs := range o.BodySection {
		buf := imapserver.ExtractBodySection(bytes.NewReader(msg.Raw), bs)
		if err := writeLiteral(w.WriteBodySection(bs, int64(len(buf))), buf); err != nil {
			return err
		}
	}
	for _, bs := range o.BinarySection {
		buf := imapserver.ExtractBinarySection(bytes.NewReader(msg.Raw), bs)
		if err := writeLiteral(w.WriteBinarySection(bs, int64(len(buf))), buf); err != nil {
			return err
		}
	}
	for _, bs := range o.BinarySectionSize {
		w.WriteBinarySectionSize(bs, imapserver.ExtractBinarySectionSize(bytes.NewReader(msg.Raw), bs))
	}
	return w.Close()
}

func writeLiteral(wc io.WriteCloser, buf []byte) error {
	_, werr := wc.Write(buf)
	cerr := wc.Close()
	return errors.Join(werr, cerr)
}

func (s *session) setFlags(id int64, op *imap.StoreFlags) {
	cur := s.flags[id]
	if cur == nil || op.Op == imap.StoreFlagsSet {
		cur = map[imap.Flag]struct{}{}
	}
	for _, f := range op.Flags {
		switch op.Op {
		case imap.StoreFlagsDel:
			delete(cur, canonical(f))
		default:
			cur[canonical(f)] = struct{}{}
		}
	}
	s.flags[id] = cur
}

func (s *session) Store(w *imapserver.FetchWriter, numSet imap.NumSet, flags *imap.StoreFlags, _ *imap.StoreOptions) error {
	sel, err := s.requireSelected()
	if err != nil {
		return err
	}
	if sel.readOnly {
		return errReadOnly
	}
	seqs := sel.seqNums(numSet)
	for _, seq := range seqs {
		s.setFlags(sel.ids[seq-1], flags)
	}
	if flags.Silent {
		return nil
	}
	for _, seq := range seqs {
		id := sel.ids[seq-1]
		rw := w.CreateMessage(seq)
		rw.WriteUID(imap.UID(id))
		rw.WriteFlags(s.flagList(id))
		if err := rw.Close(); err != nil {
			return err
		}
	}
	return nil
}

func (s *session) Expunge(w *imapserver.ExpungeWriter, uids *imap.UIDSet) error {
	sel, err := s.requireSelected()
	if err != nil {
		return err
	}
	if sel.readOnly {
		return errReadOnly
	}

	var doomed []int64
	var seqs []uint32
	for i, id := range sel.ids {
		if uids != nil && !uids.Contains(imap.UID(id)) {
			continue
		}
		if s.hasFlag(id, imap.FlagDeleted) {
			doomed = append(doomed, id)
			seqs = append(seqs, uint32(i)+1)
		}
	}
	if len(doomed) == 0 {
		return nil
	}
	if err := s.srv.db.DeleteMessages(s.srv.ctx(), doomed); err != nil {
		return err
	}
	s.srv.hub.Broadcast(ws.Event{Type: "message:deleted", Data: map[string]any{"message_ids": doomed}})

	// Report from the highest sequence number down so earlier numbers stay valid.
	for i := len(seqs) - 1; i >= 0; i-- {
		if err := w.WriteExpunge(seqs[i]); err != nil {
			return err
		}
	}
	gone := make(map[int64]struct{}, len(doomed))
	for _, id := range doomed {
		gone[id] = struct{}{}
		delete(s.flags, id)
	}
	kept := sel.ids[:0]
	for _, id := range sel.ids {
		if _, ok := gone[id]; !ok {
			kept = append(kept, id)
		}
	}
	sel.ids = kept
	return nil
}

// -- Search --

func (s *session) Search(kind imapserver.NumKind, criteria *imap.SearchCriteria, _ *imap.SearchOptions) (*imap.SearchData, error) {
	sel, err := s.requireSelected()
	if err != nil {
		return nil, err
	}
	var (
		data   imap.SearchData
		seqSet imap.SeqSet
		uidSet imap.UIDSet
	)
	for i, id := range sel.ids {
		seq := uint32(i) + 1
		ok, err := s.matches(sel, seq, id, criteria)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		seqSet.AddNum(seq)
		uidSet.AddNum(imap.UID(id))
		num := seq
		if kind == imapserver.NumKindUID {
			num = uint32(id)
		}
		if data.Min == 0 || num < data.Min {
			data.Min = num
		}
		if num > data.Max {
			data.Max = num
		}
		data.Count++
	}
	if kind == imapserver.NumKindUID {
		data.All = uidSet
	} else {
		data.All = seqSet
	}
	return &data, nil
}

func (s *session) matches(sel *selection, seq uint32, id int64, c *imap.SearchCriteria) (bool, error) {
	max := uint32(len(sel.ids))
	for _, set := range c.SeqNum {
		if !contains(set, seq, max) {
			return false, nil
		}
	}
	var maxUID uint32
	if len(sel.ids) > 0 {
		maxUID = uint32(sel.ids[len(sel.ids)-1])
	}
	for _, set := range c.UID {
		if !containsUID(set, uint32(id), maxUID) {
			return false, nil
		}
	}
	for _, f := range c.Flag {
		if !s.hasFlag(id, f) {
			return false, nil
		}
	}
	for _, f := range c.NotFlag {
		if s.hasFlag(id, f) {
			return false, nil
		}
	}

	needRaw := len(c.Header) > 0 || len(c.Body) > 0 || len(c.Text) > 0 || !c.SentSince.IsZero() || !c.SentBefore.IsZero()
	msg, err := s.srv.db.GetIMAPMessage(s.srv.ctx(), id, needRaw)
	if err != nil || msg == nil {
		return false, err
	}
	if !dateIn(msg.CreatedAt, c.Since, c.Before) {
		return false, nil
	}
	if c.Larger != 0 && int64(msg.Size) <= c.Larger {
		return false, nil
	}
	if c.Smaller != 0 && int64(msg.Size) >= c.Smaller {
		return false, nil
	}

	if needRaw {
		raw := bytes.ToLower(msg.Raw)
		headerEnd := bytes.Index(raw, []byte("\r\n\r\n"))
		if headerEnd < 0 {
			headerEnd = bytes.Index(raw, []byte("\n\n"))
		}
		if headerEnd < 0 {
			headerEnd = len(raw)
		}
		body := raw[headerEnd:]

		for _, t := range c.Text {
			if !bytes.Contains(raw, []byte(strings.ToLower(t))) {
				return false, nil
			}
		}
		for _, b := range c.Body {
			if !bytes.Contains(body, []byte(strings.ToLower(b))) {
				return false, nil
			}
		}
		if len(c.Header) > 0 || !c.SentSince.IsZero() || !c.SentBefore.IsZero() {
			m, err := mail.ReadMessage(bytes.NewReader(msg.Raw))
			if err != nil {
				return false, nil
			}
			for _, h := range c.Header {
				found := false
				for _, v := range m.Header[textproto.CanonicalMIMEHeaderKey(h.Key)] {
					if strings.Contains(strings.ToLower(v), strings.ToLower(h.Value)) {
						found = true
					}
				}
				if !found {
					return false, nil
				}
			}
			if !c.SentSince.IsZero() || !c.SentBefore.IsZero() {
				t, err := m.Header.Date()
				if err != nil || !dateIn(t, c.SentSince, c.SentBefore) {
					return false, nil
				}
			}
		}
	}

	for i := range c.Not {
		ok, err := s.matches(sel, seq, id, &c.Not[i])
		if err != nil || ok {
			return false, err
		}
	}
	for i := range c.Or {
		a, err := s.matches(sel, seq, id, &c.Or[i][0])
		if err != nil {
			return false, err
		}
		b, err := s.matches(sel, seq, id, &c.Or[i][1])
		if err != nil {
			return false, err
		}
		if !a && !b {
			return false, nil
		}
	}
	return true, nil
}

// dateIn compares calendar dates only, as RFC 3501 requires.
func dateIn(t, since, before time.Time) bool {
	t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	if !since.IsZero() && t.Before(since) {
		return false
	}
	if !before.IsZero() && !t.Before(before) {
		return false
	}
	return true
}

// -- Updates --

// refresh reloads the selection's message ids and writes the changes to the
// client: messages deleted elsewhere (only when allowed) and new arrivals.
func (s *session) refresh(w *imapserver.UpdateWriter, allowExpunge bool) error {
	sel := s.sel
	if sel == nil {
		return nil
	}
	current, err := s.srv.db.ListMessageIDs(s.srv.ctx(), sel.mailboxID)
	if err != nil {
		return err
	}
	have := make(map[int64]struct{}, len(current))
	for _, id := range current {
		have[id] = struct{}{}
	}

	if allowExpunge {
		var kept []int64
		var gone []uint32
		for i, id := range sel.ids {
			if _, ok := have[id]; ok {
				kept = append(kept, id)
			} else {
				gone = append(gone, uint32(i)+1)
			}
		}
		for i := len(gone) - 1; i >= 0; i-- {
			if err := w.WriteExpunge(gone[i]); err != nil {
				return err
			}
		}
		sel.ids = kept
	}

	// Ids only grow, so anything above the last known id is new.
	var last int64
	if n := len(sel.ids); n > 0 {
		last = sel.ids[n-1]
	}
	added := false
	for _, id := range current {
		if id > last {
			sel.ids = append(sel.ids, id)
			added = true
		}
	}
	if added || (allowExpunge && len(sel.ids) != len(current)) {
		return w.WriteNumMessages(uint32(len(sel.ids)))
	}
	return nil
}

func (s *session) Poll(w *imapserver.UpdateWriter, allowExpunge bool) error {
	return s.refresh(w, allowExpunge)
}

func (s *session) Idle(w *imapserver.UpdateWriter, stop <-chan struct{}) error {
	events, cancel := s.srv.hub.Subscribe()
	defer cancel()

	// Re-check periodically too: a full subscriber buffer drops events.
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()

	for {
		select {
		case <-stop:
			return nil
		case ev := <-events:
			if ev.Type != "message:new" && ev.Type != "message:deleted" && ev.Type != "mailbox:deleted" {
				continue
			}
		case <-tick.C:
		}
		if err := s.refresh(w, true); err != nil {
			return err
		}
	}
}
