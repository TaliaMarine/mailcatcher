export interface Mailbox {
  id: number
  address: string
  name: string
  created_at: string
  message_count: number
  last_message_at: string
}

export interface MessageSummary {
  id: number
  from_address: string
  from_name: string
  subject: string
  size: number
  has_attachments: boolean
  created_at: string
}

export interface MessageRecipient {
  address: string
  name: string
  recipient_type: 'to' | 'cc' | 'bcc'
}

export interface MessageDetail {
  id: number
  message_id: string
  from_address: string
  from_name: string
  subject: string
  body_text: string
  body_html: string | null
  size: number
  created_at: string
  attachments: AttachmentMeta[]
  recipients: MessageRecipient[]
}

export interface AttachmentMeta {
  id: number
  message_id: number
  filename: string
  content_type: string
  size: number
}

export interface UserInfo {
  email: string
  name: string
  preferred_username: string
  can_sign_out: boolean
}

export interface ImapAccess {
  enabled: boolean
  configured: boolean
  username?: string
  updated_at?: string
  host?: string
  tls_port?: number
  starttls_port?: number
}

export interface WsEvent {
  type: 'message:new' | 'message:deleted' | 'mailbox:deleted'
  data: Record<string, unknown>
}
