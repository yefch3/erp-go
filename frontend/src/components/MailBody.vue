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
    sandbox="allow-same-origin allow-popups allow-popups-to-escape-sandbox"
    referrerpolicy="no-referrer"
    :aria-label="t('reader.bodyFrame')"
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

// allow-same-origin, and deliberately never allow-scripts.
//
// The isolation that matters here is CSS, and that comes from the frame
// itself rather than from the origin: two documents never share styles,
// whatever their origins. What allow-same-origin grants is script access —
// and no script can run, because allow-scripts is absent. The pairing that is
// genuinely dangerous is allow-same-origin *with* allow-scripts, where the
// frame can reach up and remove its own sandbox attribute; without scripts
// there is nothing to do the reaching.
//
// It was left off at first, on the assumption that the parent could still
// read contentDocument for the height. It cannot: without allow-same-origin
// the document is opaque to us, every measurement threw, and the frame sat at
// its fallback height with the mail scrolling inside a box — which is exactly
// what a mail client should never look like.
//
// allow-popups is there so a link still opens; without it a sandboxed frame
// swallows target=_blank silently, and a customer's link doing nothing is
// indistinguishable from a broken client.
const doc = computed(() => {
  return `<!doctype html><html><head><meta charset="utf-8">
<base target="_blank">
<style>
  /* The ground the mail sits on, and the reason it reads as a mail rather
     than as loose content on our page. Almost every mail paints its own
     background on a table or a wrapper narrower than the window, so what
     shows around it is this — the same relationship Gmail has between its
     grey page and the white letter on top of it. On our side the mail bled
     into the page and its edges disappeared.

     Declared here in the head, so a sender who sets their own body background
     still wins: their stylesheet comes later in the document. A literal
     rather than our theme token because the frame is a separate document and
     cannot see the application's CSS variables. */
  html,body{margin:0;padding:0;background:#f1f3f4;}
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
  // No overflow:hidden inside the frame, deliberately. The frame is sized to
  // its content so no scrollbar appears anyway, and hiding the overflow would
  // mean that a measurement which went wrong silently clips the mail instead
  // of leaving it reachable. Wrong-but-scrollable beats wrong-but-hidden.
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
  /* No card and no border: the separation comes from the ground inside the
     frame, not from a box around it. Matched here so there is no white flash
     between the frame appearing and its document painting. */
  background: #f1f3f4;
}
</style>
