<template>
  <!-- The mail renders inside a sandboxed frame rather than in our document.
       Two things follow from that, and they are the whole reason this
       component exists.

       The sender keeps their stylesheet. A marketing mail is a stylesheet
       plus a scaffold of classed divs; injected into our page that stylesheet
       would be a stranger writing CSS for the ERP — `body { display:none }`
       hides the app, `position:fixed` puts their content over our toolbar.
       Inside the frame it reaches nothing but their own document, so it can
       simply be allowed, and the mail looks like itself.

       And our styling stops leaking into theirs. The reader used to set a
       14px font and an ERP-blue link colour that cascaded into every received
       mail, shrinking a sender's display type and repainting their brand
       colour. A frame has its own document; nothing crosses. -->
  <iframe
    ref="frame"
    class="mail-frame"
    :srcdoc="doc"
    :style="{ height: height + 'px' }"
    sandbox="allow-popups allow-popups-to-escape-sandbox"
    referrerpolicy="no-referrer"
    :title="t('reader.bodyFrame')"
    @load="measure"
  />
</template>

<script setup lang="ts">
import { computed, ref, watch, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const props = defineProps<{ html: string }>()

const frame = ref<HTMLIFrameElement | null>(null)
// A first guess, replaced the moment the frame reports its real content
// height. Small enough not to leave a gap under a one-line mail, big enough
// that the common case does not visibly grow.
const height = ref(320)

// No allow-scripts, and no allow-same-origin.
//
// Together those two make the frame's origin opaque, so nothing inside can
// reach our cookies, our DOM or our session even if the sanitiser one day
// misses something. It also means no script of ours can run inside to report
// the content height, which is the usual way this is done — so the height is
// measured from out here instead, which is possible only because srcdoc
// content without allow-same-origin is still same-origin *to the parent* for
// document access in every engine we target. Where it is not, the fallback
// below keeps the mail readable rather than clipped.
//
// allow-popups is there so a link still opens; without it a sandboxed frame
// swallows target=_blank silently, and a customer's link doing nothing is
// indistinguishable from a broken client.
const doc = computed(() => {
  return `<!doctype html><html><head><meta charset="utf-8">
<base target="_blank">
<style>
  html,body{margin:0;padding:0;}
  /* The mail's own width, not ours. Images are capped so a 2000px banner
     cannot force a horizontal scrollbar, which is the one piece of styling
     worth imposing. */
  body{font-family:system-ui,-apple-system,"Segoe UI",Roboto,"Helvetica Neue",Arial,sans-serif;}
  img{max-width:100%;height:auto;}
  table{max-width:100%;}
</style>
</head><body>${props.html}</body></html>`
})

let poll: number | undefined

// Measured after load, then a few more times, because images arrive later and
// each one changes the height. Polling briefly beats a ResizeObserver here:
// the observer would have to live inside the frame, and inside the frame is
// exactly where we have chosen not to run scripts.
function measure() {
  read()
  window.clearInterval(poll)
  let ticks = 0
  poll = window.setInterval(() => {
    read()
    if (++ticks > 12) window.clearInterval(poll)
  }, 250)
}

function read() {
  const el = frame.value
  if (!el) return
  try {
    const d = el.contentDocument
    if (!d?.body) return
    const h = Math.max(d.body.scrollHeight, d.documentElement?.scrollHeight ?? 0)
    if (h > 0) height.value = h + 8
  } catch {
    // Cross-origin refusal. Rather than clip the mail, give it room and let
    // the frame scroll internally — worse to read, but nothing is hidden.
    height.value = 800
  }
}

watch(() => props.html, () => { height.value = 320 })
onBeforeUnmount(() => window.clearInterval(poll))
</script>

<style scoped>
.mail-frame {
  display: block;
  width: 100%;
  border: 0;
  /* The frame is the mail's surface. A hairline and a white ground are what
     separate the sender's content from our chrome — without them a mail with
     no background of its own bleeds into the page and the reader cannot tell
     where our UI stops and a stranger's begins. */
  background: #fff;
  border-radius: 6px;
}
</style>
