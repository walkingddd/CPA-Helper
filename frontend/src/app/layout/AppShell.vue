<script setup lang="ts">
import type { Component } from 'vue'
import { computed, h, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { isNavigationFailure, NavigationFailureType, useRoute, useRouter } from 'vue-router'
import {
  NButton,
  NDrawer,
  NDrawerContent,
  NIcon,
  NLayout,
  NLayoutContent,
  NLayoutHeader,
  NLayoutSider,
  NMenu,
  NTooltip,
  useMessage,
  type MenuOption,
} from 'naive-ui'
import {
  Activity,
  BarChart3,
  Cpu,
  DollarSign,
  Gauge,
  Github,
  KeyRound,
  Languages,
  List,
  ListChecks,
  LogOut,
  Menu,
  Monitor,
  Moon,
  RadioTower,
  Settings,
  Shield,
  Store,
  Sun,
  UserRound,
  Users,
} from 'lucide-vue-next'

import { getMe, isAuthUser, logout } from '@/features/auth/api/authApi'
import { useCurrentUser } from '@/features/auth/state/currentUser'
import { useThemePreference } from '@/shared/composables/useThemePreference'
import { useI18n } from '@/shared/i18n'
import { logoUrl } from '@/shared/utils/assets'

const route = useRoute()
const router = useRouter()
const message = useMessage()
const repositoryUrl = 'https://github.com/walkingddd/CPA-Helper'
const mobileQuery = window.matchMedia('(max-width: 860px)')
const isMobile = ref(mobileQuery.matches)
const drawerOpen = ref(false)
const navigationTarget = ref<string | null>(null)
const isRouteTransitioning = ref(false)
const { currentUser, setCurrentUser } = useCurrentUser()
const hasLoadedUser = ref(currentUser.value !== null)
const { isDark, preference, setThemePreference, toggleTheme } = useThemePreference()
const { language, t, toggleLanguage } = useI18n()
let navigationFeedbackTimer: number | undefined
let routeTransitionReleaseTimer: number | undefined

function handleMobileChange(event: MediaQueryListEvent) {
  isMobile.value = event.matches
  if (!event.matches) {
    drawerOpen.value = false
  }
}

mobileQuery.addEventListener('change', handleMobileChange)
onBeforeUnmount(() => {
  mobileQuery.removeEventListener('change', handleMobileChange)
  if (navigationFeedbackTimer !== undefined) {
    window.clearTimeout(navigationFeedbackTimer)
  }
  if (routeTransitionReleaseTimer !== undefined) {
    window.clearTimeout(routeTransitionReleaseTimer)
  }
})

async function refreshCurrentUser() {
  try {
    setCurrentUser(await getMe())
  } catch {
    setCurrentUser(null)
  } finally {
    hasLoadedUser.value = true
  }
}

onMounted(() => {
  void refreshCurrentUser()
})

function handleAccountUpdated(event: Event) {
  const nextUser = (event as CustomEvent<unknown>).detail
  if (isAuthUser(nextUser)) {
    setCurrentUser(nextUser)
    hasLoadedUser.value = true
    return
  }
  void refreshCurrentUser()
}

window.addEventListener('cpa:account-updated', handleAccountUpdated)
onBeforeUnmount(() => {
  window.removeEventListener('cpa:account-updated', handleAccountUpdated)
})

function renderIcon(icon: Component) {
  return () =>
    h(
      NIcon,
      { size: 18 },
      {
        default: () => h(icon),
      },
    )
}

const adminMenuItems = computed<MenuOption[]>(() => [
  { label: t('历史用量', 'Usage History'), key: '/admin/usage', icon: renderIcon(BarChart3) },
  { label: t('请求明细', 'Request Records'), key: '/admin/records', icon: renderIcon(List) },
  { label: t('用户管理', 'Users'), key: '/admin/users', icon: renderIcon(Users) },
  { label: t('模型价格', 'Model Prices'), key: '/admin/pricing', icon: renderIcon(DollarSign) },
  { label: t('卡网收录', 'Card Shops'), key: '/admin/card-shops', icon: renderIcon(Store) },
  { label: t('系统设置', 'System Settings'), key: '/admin/settings', icon: renderIcon(Settings) },
])

const accountInspectionMenuItems = computed<MenuOption[]>(() => [
  {
    label: t('巡检设置', 'Inspection Settings'),
    key: '/admin/account-inspection',
    icon: renderIcon(Activity),
  },
  { label: t('额度分配', 'Account Allocations'), key: '/admin/account-allocations', icon: renderIcon(Gauge) },
  { label: t('账号状态', 'Account Status'), key: '/admin/account-status', icon: renderIcon(ListChecks) },
])

const accountMenuItems = computed<MenuOption[]>(() => [
  { label: t('我的用量', 'My Usage'), key: '/account/usage', icon: renderIcon(BarChart3) },
  { label: t('我的明细', 'My Records'), key: '/account/records', icon: renderIcon(List) },
  { label: t('Channel Status', 'Channel Status'), key: '/account/channel-status', icon: renderIcon(RadioTower) },
  { label: t('API 密钥', 'API Keys'), key: '/account/keys', icon: renderIcon(KeyRound) },
  { label: t('可用模型', 'Available Models'), key: '/account/models', icon: renderIcon(Cpu) },
  { label: t('账户设置', 'Account Settings'), key: '/account/settings', icon: renderIcon(UserRound) },
])

const isAdmin = computed(() => {
  if (currentUser.value) {
    return currentUser.value.is_admin
  }
  if (!hasLoadedUser.value) {
    return route.path.startsWith('/admin')
  }
  return false
})
const roleText = computed(() => (isAdmin.value ? t('管理员', 'Admin') : t('普通用户', 'User')))
const accountText = computed(() => currentUser.value?.username || t('当前账号', 'Current account'))

function formatAppVersion(value: string | undefined): string {
  const version = value?.trim()
  if (!version) {
    return 'dev'
  }
  if (version === 'latest' || version === 'dev') {
    return version
  }
  return version.startsWith('v') ? version : `v${version}`
}

const appVersion = formatAppVersion(import.meta.env.VITE_APP_VERSION)

const menuOptions = computed<MenuOption[]>(() => {
  const groups: MenuOption[] = []
  if (isAdmin.value) {
    groups.push({
      type: 'group',
      label: t('管理中心', 'Admin Center'),
      key: 'admin-group',
      icon: renderIcon(Shield),
      children: adminMenuItems.value,
    })
    groups.push({
      type: 'group',
      label: t('账号巡检', 'Account Inspection'),
      key: 'account-inspection-group',
      icon: renderIcon(Activity),
      children: accountInspectionMenuItems.value,
    })
  }
  groups.push({
    type: 'group',
    label: t('我的账户', 'My Account'),
    key: 'account-group',
    icon: renderIcon(UserRound),
    children: accountMenuItems.value,
  })
  return groups
})

const leafMenuOptions = computed(() =>
  isAdmin.value
    ? [...adminMenuItems.value, ...accountInspectionMenuItems.value, ...accountMenuItems.value]
    : accountMenuItems.value,
)

const selectedKey = computed(() => {
  const matched = leafMenuOptions.value.find((item) => route.path.startsWith(String(item.key)))
  return matched ? String(matched.key) : isAdmin.value ? '/admin/usage' : '/account/usage'
})
const isMenuNavigationPending = computed(() => navigationTarget.value !== null)
const recordsRoutePaths = ['/admin/records', '/account/records'] as const
const isRecordsScrollMode = computed(
  () =>
    recordsRoutePaths.some((path) => route.path === path) ||
    (navigationTarget.value !== null &&
      recordsRoutePaths.some((path) => navigationTarget.value === path)),
)

function finishNavigationFeedback(target: string) {
  if (navigationFeedbackTimer !== undefined) {
    window.clearTimeout(navigationFeedbackTimer)
  }
  navigationFeedbackTimer = window.setTimeout(() => {
    if (navigationTarget.value === target) {
      navigationTarget.value = null
    }
  }, 180)
}

function beginRouteTransition() {
  if (routeTransitionReleaseTimer !== undefined) {
    window.clearTimeout(routeTransitionReleaseTimer)
    routeTransitionReleaseTimer = undefined
  }
  isRouteTransitioning.value = true
}

function finishRouteTransition() {
  if (routeTransitionReleaseTimer !== undefined) {
    window.clearTimeout(routeTransitionReleaseTimer)
  }
  routeTransitionReleaseTimer = window.setTimeout(() => {
    isRouteTransitioning.value = false
    routeTransitionReleaseTimer = undefined
  }, 60)
}

async function handleMenuUpdate(key: string) {
  drawerOpen.value = false
  if (key === route.path) {
    return
  }
  navigationTarget.value = key
  await nextTick()
  try {
    const result = await router.push(key)
    if (
      isNavigationFailure(result, NavigationFailureType.cancelled) &&
      route.path !== key
    ) {
      await router.push(key)
    }
  } finally {
    finishNavigationFeedback(key)
  }
}

async function handleLogout() {
  await logout()
  setCurrentUser(null)
  hasLoadedUser.value = true
  message.success(t('已退出登录', 'Signed out'))
  await router.push('/login')
}

function cycleTheme() {
  if (preference.value === 'system') {
    setThemePreference('light')
    return
  }
  if (preference.value === 'light') {
    setThemePreference('dark')
    return
  }
  setThemePreference('system')
}

const themeIcon = computed(() => {
  if (preference.value === 'system') {
    return Monitor
  }
  return isDark.value ? Moon : Sun
})

const languageLabel = computed(() => (language.value === 'zh' ? 'EN' : 'CN'))
const languageAriaLabel = computed(() => t('切换语言', 'Switch language'))
const themeAriaLabel = computed(() => t('切换主题', 'Switch theme'))
const logoutAriaLabel = computed(() => t('退出登录', 'Sign out'))
</script>

<template>
  <NLayout class="app-shell" has-sider>
    <NLayoutSider
      v-if="!isMobile"
      class="app-sider"
      bordered
      :width="228"
      collapse-mode="width"
    >
      <div class="brand">
        <div class="brand-mark">
          <img :src="logoUrl" alt="">
        </div>
        <div class="brand-copy">
          <strong>CPA-Helper</strong>
          <span>{{ accountText }} · {{ roleText }}</span>
        </div>
      </div>
      <NMenu
        class="sider-menu"
        :value="selectedKey"
        :options="menuOptions"
        :root-indent="18"
        :indent="12"
        @update:value="handleMenuUpdate"
      />
      <div class="sider-footer">
        <a
          class="sider-version-link"
          :href="repositoryUrl"
          target="_blank"
          rel="noreferrer"
          :aria-label="t('在 GitHub 查看 CPA-Helper', 'View CPA-Helper on GitHub')"
        >
          <NIcon :component="Github" :size="20" />
          <span class="sider-version-text">{{ appVersion }}</span>
        </a>
        <div class="sider-actions">
          <NTooltip trigger="hover">
            <template #trigger>
              <NButton quaternary circle :aria-label="languageAriaLabel" @click="toggleLanguage">
                <template #icon>
                  <NIcon :component="Languages" />
                </template>
                <span class="sr-only">{{ languageLabel }}</span>
              </NButton>
            </template>
            {{ language === 'zh' ? 'English' : '中文' }}
          </NTooltip>
          <NTooltip trigger="hover">
            <template #trigger>
              <NButton quaternary circle :aria-label="themeAriaLabel" @click="cycleTheme">
                <template #icon>
                  <NIcon :component="themeIcon" />
                </template>
              </NButton>
            </template>
            {{ t('主题', 'Theme') }}
          </NTooltip>
          <NTooltip trigger="hover">
            <template #trigger>
              <NButton quaternary circle :aria-label="logoutAriaLabel" @click="handleLogout">
                <template #icon>
                  <NIcon :component="LogOut" />
                </template>
              </NButton>
            </template>
            {{ t('退出', 'Sign out') }}
          </NTooltip>
        </div>
      </div>
    </NLayoutSider>

    <NLayout class="app-main">
      <NLayoutHeader class="mobile-header" bordered>
        <NButton quaternary circle :aria-label="t('打开导航', 'Open navigation')" @click="drawerOpen = true">
          <template #icon>
            <NIcon :component="Menu" />
          </template>
        </NButton>
        <div class="mobile-brand" :aria-label="t('CPA-Helper 账号信息', 'CPA-Helper account info')">
          <img class="mobile-brand-logo" :src="logoUrl" alt="" aria-hidden="true">
          <div class="mobile-brand-copy">
            <div class="mobile-title-row">
              <strong>CPA-Helper</strong>
              <span class="mobile-version-badge">{{ appVersion }}</span>
            </div>
            <span>{{ accountText }} · {{ roleText }}</span>
          </div>
        </div>
        <div class="mobile-actions">
          <NButton quaternary circle :aria-label="languageAriaLabel" @click="toggleLanguage">
            <template #icon>
              <NIcon :component="Languages" />
            </template>
            <span class="sr-only">{{ languageLabel }}</span>
          </NButton>
          <NButton quaternary circle :aria-label="themeAriaLabel" @click="toggleTheme">
            <template #icon>
              <NIcon :component="themeIcon" />
            </template>
          </NButton>
        </div>
      </NLayoutHeader>
      <NLayoutContent
        class="content"
        :class="{
          'is-route-pending': isMenuNavigationPending,
          'is-route-transitioning': isRouteTransitioning,
          'is-records-scroll-mode': isRecordsScrollMode,
        }"
      >
        <div v-if="isMenuNavigationPending" class="route-progress" aria-hidden="true" />
        <RouterView v-slot="{ Component: RouteComponent, route: activeRoute }">
          <Transition
            name="route-fade"
            mode="out-in"
            appear
            @before-enter="beginRouteTransition"
            @after-enter="finishRouteTransition"
            @enter-cancelled="finishRouteTransition"
            @before-leave="beginRouteTransition"
            @after-leave="finishRouteTransition"
            @leave-cancelled="finishRouteTransition"
          >
            <component :is="RouteComponent" :key="activeRoute.name ?? activeRoute.path" />
          </Transition>
        </RouterView>
      </NLayoutContent>
    </NLayout>

    <NDrawer v-model:show="drawerOpen" placement="left" :width="248">
      <NDrawerContent :title="`CPA-Helper · ${appVersion}`" body-content-style="padding: 0;">
        <NMenu :value="selectedKey" :options="menuOptions" @update:value="handleMenuUpdate" />
        <div class="drawer-actions">
          <NButton secondary @click="toggleLanguage">
            <template #icon>
              <NIcon :component="Languages" />
            </template>
            {{ language === 'zh' ? 'English' : '中文' }}
          </NButton>
          <NButton secondary @click="cycleTheme">
            <template #icon>
              <NIcon :component="themeIcon" />
            </template>
            {{ t('主题', 'Theme') }}
          </NButton>
          <NButton secondary @click="handleLogout">
            <template #icon>
              <NIcon :component="LogOut" />
            </template>
            {{ t('退出', 'Sign out') }}
          </NButton>
        </div>
      </NDrawerContent>
    </NDrawer>
  </NLayout>
</template>

<style scoped>
.app-shell {
  height: 100vh;
  height: 100dvh;
  min-height: 0;
  overflow: hidden;
  --n-color: var(--cpa-bg);
  background: var(--cpa-bg);
}

.app-shell > :deep(.n-layout-scroll-container),
.app-main > :deep(.n-layout-scroll-container) {
  overflow: hidden;
  scrollbar-gutter: auto;
  scrollbar-width: none;
  background: var(--cpa-bg);
}

.app-main > :deep(.n-layout-scroll-container) {
  display: flex;
  min-height: 0;
  flex-direction: column;
}

.app-shell > :deep(.n-layout-scroll-container::-webkit-scrollbar),
.app-main > :deep(.n-layout-scroll-container::-webkit-scrollbar) {
  display: none;
}

.app-sider {
  position: relative;
  height: 100vh;
  height: 100dvh;
  max-height: 100vh;
  max-height: 100dvh;
  border-right: 1px solid var(--cpa-border);
  background:
    linear-gradient(180deg, rgb(255 255 255 / 96%) 0, rgb(255 255 255 / 86%) 100%),
    var(--cpa-surface-solid);
  box-shadow: 18px 0 38px rgb(30 56 62 / 7%);
  backdrop-filter: blur(22px);
}

.app-sider :deep(.n-layout-sider-scroll-container) {
  display: flex;
  height: 100%;
  min-height: 0;
  flex-direction: column;
  overflow: hidden;
}

:root.dark .app-sider {
  background:
    linear-gradient(180deg, rgb(26 42 48 / 88%) 0, rgb(18 30 35 / 78%) 100%),
    var(--cpa-glass);
  box-shadow: 14px 0 34px rgb(0 0 0 / 22%);
}

.brand {
  display: flex;
  flex: 0 0 auto;
  gap: 12px;
  align-items: center;
  padding: 28px 18px 24px;
}

.brand-mark {
  display: grid;
  width: 46px;
  height: 46px;
  place-items: center;
  border-radius: 14px;
  overflow: hidden;
  background: var(--cpa-surface-solid);
  box-shadow: 0 14px 26px rgb(0 154 168 / 20%);
}

.brand-mark img {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.brand-copy {
  display: grid;
  min-width: 0;
  gap: 3px;
}

.mobile-title-row {
  display: flex;
  min-width: 0;
  align-items: center;
}

.brand-copy strong {
  color: var(--cpa-text-strong);
  font-size: 17px;
  font-weight: 760;
  line-height: 1.2;
}

.mobile-version-badge {
  display: inline-flex;
  align-items: center;
  flex: 0 0 auto;
  max-width: 72px;
  height: 18px;
  padding: 0 6px;
  border: 1px solid var(--cpa-border);
  border-radius: 999px;
  overflow: hidden;
  color: var(--cpa-text-muted);
  background: var(--cpa-surface-raised);
  font-size: 11px;
  font-weight: 750;
  line-height: 1;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.brand-copy > span {
  color: var(--cpa-text-muted);
  font-size: 12px;
}

.sider-menu {
  flex: 1 1 auto;
  min-height: 0;
  overflow-x: hidden;
  overflow-y: auto;
  scrollbar-width: thin;
  scrollbar-color: color-mix(in srgb, var(--cpa-text-muted) 34%, transparent) transparent;
}

.app-sider :deep(.n-menu) {
  padding: 0 14px 12px;
}

.app-sider :deep(.n-menu-item-group-title) {
  height: 32px;
  padding: 16px 8px 7px !important;
  color: var(--cpa-text-muted);
  font-size: 12px;
  font-weight: 750;
}

.app-sider :deep(.n-menu-item-content) {
  height: 42px;
  border-radius: var(--cpa-radius);
  overflow: hidden;
  background: transparent;
  transition:
    background-color 160ms ease,
    color 160ms ease,
    transform 160ms ease;
}

.app-sider :deep(.n-menu-item-content::before) {
  right: 0;
  left: 0;
  border-radius: var(--cpa-radius);
}

.app-sider :deep(.n-menu-item-content--selected) {
  background: transparent;
  color: var(--cpa-primary);
  font-weight: 760;
  transform: none;
}

.app-sider :deep(.n-menu-item-content--selected::before) {
  border: 1px solid rgb(0 154 168 / 7%);
  background:
    linear-gradient(90deg, rgb(0 154 168 / 8%), rgb(0 154 168 / 3%)),
    var(--cpa-primary-wash);
  box-shadow: inset 0 1px 0 rgb(255 255 255 / 68%);
}

:root.dark .app-sider :deep(.n-menu-item-content--selected::before) {
  border-color: rgb(34 193 200 / 20%);
  box-shadow: inset 0 1px 0 rgb(255 255 255 / 10%);
}

.sider-footer {
  display: grid;
  flex: 0 0 auto;
  gap: 10px;
  padding: 10px 16px 18px;
  background: inherit;
}

.sider-version-link {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 100%;
  box-sizing: border-box;
  min-height: 40px;
  gap: 8px;
  padding: 0 12px;
  border: 1px solid var(--cpa-border);
  border-radius: 8px;
  color: var(--cpa-text-strong);
  background: transparent;
  box-shadow: none;
  font-size: 13px;
  font-weight: 760;
  line-height: 1;
  text-decoration: none;
  transition:
    background-color 160ms ease,
    color 160ms ease,
    transform 160ms ease,
    border-color 160ms ease;
}

.sider-version-link:hover {
  border-color: color-mix(in srgb, var(--cpa-primary) 22%, var(--cpa-border));
  color: var(--cpa-primary);
  background: color-mix(in srgb, var(--cpa-primary) 5%, transparent);
  transform: none;
}

.sider-version-link:focus-visible {
  outline: 2px solid color-mix(in srgb, var(--cpa-primary) 54%, transparent);
  outline-offset: 2px;
}

.sider-version-link :deep(.n-icon) {
  flex: 0 0 auto;
}

.sider-version-text {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

:root.dark .sider-version-link {
  border-color: var(--cpa-border);
  background: transparent;
}

.sider-actions {
  display: flex;
  justify-content: space-between;
  width: 100%;
  box-sizing: border-box;
  padding: 8px 12px;
  border: 1px solid var(--cpa-border);
  border-radius: var(--cpa-radius);
  background: var(--cpa-surface-raised);
  box-shadow: var(--cpa-shadow-card), var(--cpa-shadow-hairline);
}

.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  border: 0;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
}

.app-main {
  display: flex;
  flex-direction: column;
  height: 100vh;
  height: 100dvh;
  min-height: 0;
  min-width: 0;
  overflow: hidden;
  --n-color: var(--cpa-bg);
  background: var(--cpa-bg);
}

.content {
  position: relative;
  flex: 1 1 auto;
  min-height: 0;
  min-width: 0;
  overflow: hidden;
  padding: 0;
  scrollbar-gutter: stable;
  scrollbar-width: thin;
  scrollbar-color: var(--content-scrollbar-thumb) transparent;
  --n-color: var(--cpa-bg);
  --content-scrollbar-thumb: color-mix(in srgb, var(--cpa-text-muted) 44%, transparent);
  --content-scrollbar-thumb-hover: color-mix(
    in srgb,
    var(--cpa-primary) 58%,
    var(--cpa-text-muted)
  );
  background: var(--cpa-bg);
}

.content > :deep(.n-layout-scroll-container) {
  overflow: auto;
  padding: 28px 36px 32px 28px;
  scrollbar-gutter: stable;
  scrollbar-width: thin;
  scrollbar-color: var(--content-scrollbar-thumb) transparent;
  background: var(--cpa-bg);
}

.content::-webkit-scrollbar,
.content > :deep(.n-layout-scroll-container::-webkit-scrollbar) {
  width: 18px;
  height: 18px;
}

.content::-webkit-scrollbar-track,
.content > :deep(.n-layout-scroll-container::-webkit-scrollbar-track),
.content::-webkit-scrollbar-corner,
.content > :deep(.n-layout-scroll-container::-webkit-scrollbar-corner) {
  background: transparent;
}

.content::-webkit-scrollbar-thumb,
.content > :deep(.n-layout-scroll-container::-webkit-scrollbar-thumb) {
  min-height: 56px;
  border: 6px solid transparent;
  border-radius: 999px;
  background: var(--content-scrollbar-thumb);
  background-clip: content-box;
}

.content::-webkit-scrollbar-thumb:hover,
.content > :deep(.n-layout-scroll-container::-webkit-scrollbar-thumb:hover) {
  background: var(--content-scrollbar-thumb-hover);
  background-clip: content-box;
}

.content.is-route-pending {
  cursor: progress;
}

.content.is-route-pending,
.content.is-route-transitioning,
.content.is-route-pending :deep(.n-layout-scroll-container),
.content.is-route-transitioning :deep(.n-layout-scroll-container) {
  overflow: hidden;
}

.content.is-route-pending :deep(.records-table .v-vl),
.content.is-route-transitioning :deep(.records-table .v-vl),
.content.is-route-pending :deep(.records-table .n-scrollbar-container),
.content.is-route-transitioning :deep(.records-table .n-scrollbar-container) {
  scrollbar-gutter: auto;
  scrollbar-width: none;
}

.content.is-route-pending :deep(.records-table .v-vl::-webkit-scrollbar),
.content.is-route-transitioning :deep(.records-table .v-vl::-webkit-scrollbar),
.content.is-route-pending :deep(.records-table .n-scrollbar-container::-webkit-scrollbar),
.content.is-route-transitioning :deep(.records-table .n-scrollbar-container::-webkit-scrollbar) {
  display: none;
  width: 0;
  height: 0;
}

.content.is-route-pending :deep(.records-table .n-scrollbar-rail--vertical),
.content.is-route-transitioning :deep(.records-table .n-scrollbar-rail--vertical),
.content.is-route-pending :deep(.records-table .n-scrollbar-rail--horizontal),
.content.is-route-transitioning :deep(.records-table .n-scrollbar-rail--horizontal) {
  visibility: hidden !important;
  opacity: 0 !important;
  pointer-events: none !important;
}

.content.is-records-scroll-mode,
.content.is-records-scroll-mode > :deep(.n-layout-scroll-container) {
  scrollbar-gutter: auto;
  scrollbar-width: none;
}

.content.is-records-scroll-mode::-webkit-scrollbar,
.content.is-records-scroll-mode > :deep(.n-layout-scroll-container::-webkit-scrollbar) {
  display: none;
  width: 0;
  height: 0;
}

.route-progress {
  position: absolute;
  z-index: 3;
  top: 0;
  right: 0;
  left: 0;
  height: 2px;
  overflow: hidden;
  background: rgb(0 154 168 / 12%);
  pointer-events: none;
}

.route-progress::before {
  display: block;
  width: 38%;
  height: 100%;
  border-radius: 999px;
  background: linear-gradient(90deg, var(--cpa-primary), var(--cpa-accent-blue));
  animation: route-progress-slide 900ms ease-in-out infinite;
  content: "";
}

.route-fade-enter-active,
.route-fade-leave-active {
  transition:
    opacity 180ms ease,
    transform 180ms ease;
}

.route-fade-enter-from,
.route-fade-leave-to {
  opacity: 0;
  transform: translateY(6px);
}

@keyframes route-progress-slide {
  0% {
    transform: translateX(-120%);
    opacity: 0.35;
  }

  50% {
    opacity: 0.9;
  }

  100% {
    transform: translateX(260%);
    opacity: 0.35;
  }
}

.mobile-header {
  display: none;
  align-items: center;
  grid-template-columns: 42px minmax(0, 1fr) auto;
  gap: 8px;
  height: 56px;
  padding: 0 10px;
  border-bottom: 1px solid var(--cpa-border);
  background: var(--cpa-mobile-header-bg);
  backdrop-filter: blur(18px);
}

.mobile-header :deep(.n-button) {
  width: 38px;
  height: 38px;
}

.mobile-actions {
  display: inline-flex;
  gap: 4px;
  align-items: center;
}

.mobile-brand {
  display: inline-grid;
  grid-template-columns: 28px minmax(0, auto);
  gap: 7px;
  align-items: center;
  justify-self: center;
  min-width: 0;
  max-width: calc(100vw - 116px);
}

.mobile-brand-logo {
  display: block;
  width: 28px;
  height: 28px;
  border-radius: 9px;
  object-fit: cover;
}

.mobile-brand-copy {
  display: grid;
  min-width: 0;
  gap: 1px;
  line-height: 1.1;
}

.mobile-brand-copy strong,
.mobile-brand-copy span {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mobile-title-row {
  gap: 5px;
}

.mobile-brand-copy strong {
  color: var(--cpa-text-strong);
  font-size: 14px;
  font-weight: 760;
}

.mobile-version-badge {
  max-width: 58px;
  height: 16px;
  padding: 0 5px;
  font-size: 10px;
}

.mobile-brand-copy > span {
  color: var(--cpa-text-muted);
  font-size: 11px;
  font-weight: 600;
}

.drawer-actions {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px;
  padding: 14px;
}

@media (max-width: 860px) {
  .mobile-header {
    display: grid;
  }

  .content {
    padding: 0;
  }

  .content > :deep(.n-layout-scroll-container) {
    padding: 10px 18px 12px 10px;
  }
}

@media (prefers-reduced-motion: reduce) {
  .route-progress::before {
    animation: none;
    opacity: 0.8;
    transform: none;
  }

  .route-fade-enter-active,
  .route-fade-leave-active {
    transition: opacity 80ms ease;
  }

  .route-fade-enter-from,
  .route-fade-leave-to {
    opacity: 0;
    transform: none;
  }
}
</style>
