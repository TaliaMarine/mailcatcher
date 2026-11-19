import { createRouter, createWebHistory } from 'vue-router'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/',
      redirect: '/mailboxes',
    },
    {
      path: '/mailboxes',
      name: 'mailboxes',
      component: () => import('../views/MailboxView.vue'),
    },
    {
      path: '/mailboxes/all',
      name: 'all-messages',
      component: () => import('../views/MailboxView.vue'),
    },
    {
      path: '/mailboxes/:id',
      name: 'mailbox',
      component: () => import('../views/MailboxView.vue'),
      props: true,
    },
    {
      path: '/search',
      name: 'search',
      component: () => import('../views/SearchView.vue'),
    },
    {
      path: '/settings/imap',
      name: 'imap',
      component: () => import('../views/ImapView.vue'),
    },
  ],
})

export default router
