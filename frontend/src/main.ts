import { createApp } from 'vue'
import { createPinia } from 'pinia'
// 组件按用到的引（见 vite.config 里的 Components 插件），样式仍然整份引：
// 那一份里包含命令式 API 和指令的样式，漏一个的样子是某个页面上的某个
// 对话框没有边框，不值得为 20 KB 去四十个页面里找。
import 'element-plus/dist/index.css'
import App from './App.vue'
import { router } from './router'
import { i18n, bootLocale } from './i18n'
import { useAuthStore } from './stores/auth'

const pinia = createPinia()

// 先把当前这一种语言装进来再挂载：三种语言一共 480 KB 源码，全打进主包等于
// 让每个人下载两份他永远不会看的文案。见 i18n.ts。
//
// 用 .then 而不是顶层 await：顶层 await 要求把构建目标提到 es2022，为了一行
// 写法去动整个应用的目标浏览器不划算。
void bootLocale().then(() => {
  createApp(App).use(pinia).use(router).use(i18n).mount('#app')

  // What this session is allowed to do, re-read once per load. The cached list
  // paints the menu immediately; this makes a role change — or a renamed
  // permission code, as mail:* just were — land on the next page load instead
  // of the next login.
  useAuthStore(pinia).refreshPermissions()
})
