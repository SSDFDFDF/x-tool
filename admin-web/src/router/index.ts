import { createRouter, createWebHashHistory } from 'vue-router'
import type { RouteRecordRaw } from 'vue-router'
import Overview from '../pages/Overview.vue'
import Config from '../pages/Config.vue'
import PromptTemplates from '../pages/PromptTemplates.vue'
import Upstreams from '../pages/Upstreams.vue'
import Logs from '../pages/Logs.vue'
import Login from '../pages/Login.vue'
import ModelsApi from '../pages/ModelsApi.vue'
import RootProbe from '../pages/RootProbe.vue'
import { ensureAuthStatusLoaded } from '../utils/admin-auth'

const routes: RouteRecordRaw[] = [
  {
    path: '/',
    name: 'overview',
    component: Overview,
    meta: { requiresAuth: true }
  },
  {
    path: '/config',
    name: 'config',
    component: Config,
    meta: { requiresAuth: true }
  },
  {
    path: '/prompts',
    name: 'prompt-templates',
    component: PromptTemplates,
    meta: { requiresAuth: true }
  },
  {
    path: '/upstreams',
    name: 'upstreams',
    component: Upstreams,
    meta: { requiresAuth: true }
  },
  {
    path: '/logs',
    name: 'logs',
    component: Logs,
    meta: { requiresAuth: true }
  },
  {
    path: '/probe/models',
    name: 'probe-models',
    component: ModelsApi,
    meta: { requiresAuth: true }
  },
  {
    path: '/probe/root',
    name: 'probe-root',
    component: RootProbe,
    meta: { requiresAuth: true }
  },
  {
    path: '/login',
    name: 'login',
    component: Login,
    meta: { guestOnly: true }
  },
  {
    path: '/:pathMatch(.*)*',
    redirect: '/'
  }
]

const router = createRouter({
  history: createWebHashHistory(),
  routes
})

router.beforeEach(async (to) => {
  const requiresAuth = to.matched.some((record) => Boolean(record.meta.requiresAuth))
  const guestOnly = to.matched.some((record) => Boolean(record.meta.guestOnly))
  const needsAuthCheck = requiresAuth || guestOnly

  if (!needsAuthCheck) {
    return true
  }

  let isAuthenticated = false

  try {
    isAuthenticated = await ensureAuthStatusLoaded()
  } catch (error) {
    console.error('Failed to load admin auth status.', error)
  }

  if (requiresAuth && !isAuthenticated) {
    return {
      name: 'login',
      query: to.fullPath && to.fullPath !== '/' ? { redirect: to.fullPath } : undefined
    }
  }

  if (guestOnly && isAuthenticated) {
    return { name: 'overview' }
  }

  return true
})

export default router
