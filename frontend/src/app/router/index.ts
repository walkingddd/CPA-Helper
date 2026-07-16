import { createRouter, createWebHistory } from 'vue-router'

import { getMe } from '@/features/auth/api/authApi'
import { setCurrentUser } from '@/features/auth/state/currentUser'
import type { AuthUser } from '@/shared/types/api'

function homePath(user: AuthUser): string {
  return user.is_admin ? '/admin/usage' : '/account/usage'
}

function stringMeta(to: { meta: Record<string, unknown> }, key: string): string | null {
  const value = to.meta[key]
  return typeof value === 'string' ? value : null
}

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/login',
      name: 'login',
      component: () => import('@/features/auth/views/LoginView.vue'),
      meta: { public: true },
    },
    {
      path: '/change-credentials',
      name: 'change-credentials',
      component: () => import('@/features/auth/views/ChangeCredentialsView.vue'),
    },
    {
      path: '/',
      component: () => import('@/app/layout/AppShell.vue'),
      children: [
        {
          path: '',
          redirect: '/usage',
        },
        {
          path: 'admin/usage',
          name: 'admin-usage',
          component: () => import('@/features/usage/views/UsageHistoryView.vue'),
          props: { scope: 'admin' },
          meta: { requiresAdmin: true },
        },
        {
          path: 'admin/records',
          name: 'admin-records',
          component: () => import('@/features/usage/views/UsageRecordsView.vue'),
          props: { scope: 'admin' },
          meta: { requiresAdmin: true },
        },
        {
          path: 'admin/card-shops',
          name: 'admin-card-shops',
          component: () => import('@/features/card-shops/views/CardShopsView.vue'),
          meta: { requiresAdmin: true },
        },
        {
          path: 'admin/users',
          name: 'admin-users',
          component: () => import('@/features/users/views/UserManagementView.vue'),
          meta: { requiresAdmin: true },
        },
        {
          path: 'admin/account-inspection',
          name: 'admin-account-inspection',
          component: () => import('@/features/codex-keeper/views/CodexKeeperInspectionView.vue'),
          meta: { requiresAdmin: true },
        },
        {
          path: 'admin/account-status',
          name: 'admin-account-status',
          component: () => import('@/features/codex-keeper/views/CodexKeeperStatusView.vue'),
          meta: { requiresAdmin: true },
        },
        {
          path: 'admin/account-allocations',
          name: 'admin-account-allocations',
          component: () => import('@/features/account-allocations/views/AccountAllocationsView.vue'),
          meta: { requiresAdmin: true },
        },
        {
          path: 'admin/pricing',
          name: 'admin-pricing',
          component: () => import('@/features/pricing/views/ModelPricesView.vue'),
          meta: { requiresAdmin: true },
        },
        {
          path: 'admin/settings',
          name: 'admin-settings',
          component: () => import('@/features/settings/views/SettingsView.vue'),
          meta: { requiresAdmin: true },
        },
        {
          path: 'account/usage',
          name: 'account-usage',
          component: () => import('@/features/usage/views/UsageHistoryView.vue'),
          props: { scope: 'account' },
        },
        {
          path: 'account/records',
          name: 'account-records',
          component: () => import('@/features/usage/views/UsageRecordsView.vue'),
          props: { scope: 'account' },
        },
        {
          path: 'account/channel-status',
          name: 'account-channel-status',
          component: () => import('@/features/codex-keeper/views/ChannelStatusView.vue'),
        },
        {
          path: 'account/keys',
          name: 'account-api-keys',
          component: () => import('@/features/api-keys/views/ApiKeysView.vue'),
        },
        {
          path: 'account/models',
          name: 'account-models',
          component: () => import('@/features/models/views/AvailableModelsView.vue'),
        },
        {
          path: 'account/settings',
          name: 'account-settings',
          component: () => import('@/features/settings/views/AccountSettingsView.vue'),
        },
      ],
    },
    {
      path: '/usage',
      name: 'legacy-usage',
      component: () => import('@/app/layout/AppShell.vue'),
      meta: { adminTarget: '/admin/usage', accountTarget: '/account/usage' },
    },
    {
      path: '/records',
      name: 'legacy-records',
      component: () => import('@/app/layout/AppShell.vue'),
      meta: { adminTarget: '/admin/records', accountTarget: '/account/records' },
    },
    {
      path: '/channel-status',
      name: 'legacy-channel-status',
      component: () => import('@/app/layout/AppShell.vue'),
      meta: { adminTarget: '/account/channel-status', accountTarget: '/account/channel-status' },
    },
    {
      path: '/users',
      name: 'legacy-users',
      component: () => import('@/app/layout/AppShell.vue'),
      meta: { adminTarget: '/admin/users', accountTarget: '/account/usage' },
    },
    {
      path: '/keys',
      name: 'legacy-keys',
      component: () => import('@/app/layout/AppShell.vue'),
      meta: { adminTarget: '/account/keys', accountTarget: '/account/keys' },
    },
    {
      path: '/pricing',
      name: 'legacy-pricing',
      component: () => import('@/app/layout/AppShell.vue'),
      meta: { adminTarget: '/admin/pricing', accountTarget: '/account/usage' },
    },
    {
      path: '/settings',
      name: 'legacy-settings',
      component: () => import('@/app/layout/AppShell.vue'),
      meta: { adminTarget: '/admin/settings', accountTarget: '/account/settings' },
    },
  ],
})

router.beforeEach(async (to) => {
  if (to.name === 'login') {
    return true
  }
  try {
    const user = await getMe()
    setCurrentUser(user)
    if (user.must_change_password && to.name !== 'change-credentials') {
      return { name: 'change-credentials' }
    }
    if (!user.must_change_password && to.name === 'change-credentials') {
      return homePath(user)
    }
    const target = user.is_admin ? stringMeta(to, 'adminTarget') : stringMeta(to, 'accountTarget')
    if (target) {
      return { path: target, query: to.query }
    }
    if (to.meta.requiresAdmin && !user.is_admin) {
      return { path: '/account/usage' }
    }
    return true
  } catch {
    setCurrentUser(null)
    return { name: 'login', query: { redirect: to.fullPath } }
  }
})
