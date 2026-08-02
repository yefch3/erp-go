import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import App from './App.vue'
import { router } from './router'
import { i18n } from './i18n'
import { useAuthStore } from './stores/auth'

const pinia = createPinia()
createApp(App).use(pinia).use(router).use(ElementPlus).use(i18n).mount('#app')

// What this session is allowed to do, re-read once per load. The cached list
// paints the menu immediately; this makes a role change — or a renamed
// permission code, as mail:* just were — land on the next page load instead
// of the next login.
useAuthStore(pinia).refreshPermissions()
