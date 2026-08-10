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
    :class="{ ready }"
    :srcdoc="doc"
    :style="{ height: height + 'px' }"
    sandbox="allow-same-origin allow-popups allow-popups-to-escape-sandbox"
    referrerpolicy="no-referrer"
    :aria-label="t('reader.bodyFrame')"
    @load="measure"
  />
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const props = defineProps<{ html: string }>()

const frame = ref<HTMLIFrameElement | null>(null)
// A first guess, replaced the moment the frame reports its real content
// height. Small enough not to leave a gap under a one-line mail, big enough
// that the common case does not visibly grow.
const height = ref(320)

// The frame is laid out but not painted until it has been measured once.
//
// Without this you saw the bug: for the moment between the frame appearing and
// the first measurement landing, the mail was rendered into a 320px window it
// did not fit, so it wore an internal scrollbar and a hard bottom edge — the
// boxed look this whole component exists to get rid of — and then snapped open.
// One frame of the old bug on the way to the new behaviour is worse than a
// blank moment, because the eye reads it as breakage rather than as loading.
//
// visibility, not display:none: a hidden-by-display frame is not laid out, and
// an unlaid-out document has no scrollHeight to measure. This has to be the
// kind of hidden that still does the work.
const ready = ref(false)

// The mail's ground colour, read from the stylesheet rather than repeated
// here. The frame is a separate document and cannot resolve our CSS variables,
// so the value must be inlined into its srcdoc — but it can be inlined from
// the one place that defines it, which is what stops the pane and the mail
// inside it from drifting to two slightly different greys.
const ground =
  getComputedStyle(document.documentElement).getPropertyValue('--mail-ground').trim() || '#f1f3f4'

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
  /* The ground the mail sits on, continuous with the reading pane around the
     frame, so a mail that paints its own white card reads as a letter lying on
     a desk and a mail that paints nothing simply shares the desk. Before this
     the mail bled into a white page and its edges disappeared.

     Declared here in the head, so a sender who sets their own body background
     still wins: their stylesheet comes later in the document. This is where
     the grey in the Capital One mail comes from in Gmail, incidentally —
     Gmail's own reading pane is white, and the mail paints that grey itself. */
  html,body{margin:0;padding:0;background:${ground};}
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

// Measuring starts when the mail arrives, not when the frame finishes loading.
//
// That distinction was worth several seconds. The frame stays hidden until it
// has been measured once, and measuring used to be triggered by the frame's
// load event — which does not fire until every image in the mail has arrived
// or given up. A marketing mail carries dozens of images from the sender's own
// servers; the worst one in a real mailbox here has a hundred and fourteen. So
// the reading pane sat blank, waiting on somebody else's CDN, while the text
// had been laid out and ready to read almost immediately.
//
// Nothing about the height needs those images. Text lays out first and has a
// height; each image that lands changes it, and the poll below is already
// there to follow that. Waiting for all of them before showing anything threw
// away the whole point of polling.
//
// So: reveal on the first measurement that succeeds, keep adjusting for a few
// seconds as pictures land, and take one last reading at load when the last of
// them has settled. Polling beats a ResizeObserver here because the observer
// would have to live inside the frame, and inside the frame is exactly where
// we have chosen not to run scripts.
function measure() {
  read()
  window.clearInterval(poll)
  let ticks = 0
  poll = window.setInterval(() => {
    read()
    // A hundred milliseconds, thirty times: three seconds of following the
    // layout, with the first look soon enough to feel immediate. The old
    // quarter-second interval was chosen when this only ran after everything
    // had already loaded and had nothing left to catch.
    if (++ticks > 30) {
      window.clearInterval(poll)
      // Whatever happened, the mail becomes visible. A measurement that never
      // succeeded would otherwise leave the frame hidden for good, and an
      // invisible mail is a far worse failure than a badly sized one.
      ready.value = true
    }
  }, 100)
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
    // The body's own height, and only the body's.
    //
    // This used to take Math.max of the body and the documentElement, and
    // that was a feedback loop with a very specific symptom: the mail crept
    // downwards while you were reading it. documentElement.scrollHeight is at
    // least the frame's viewport height, and the frame's height is whatever we
    // set last time — so every measurement re-read its own previous answer and
    // added the slop again. Thirty-two ticks at eight pixels is 256px of
    // drift, which is exactly what was measured on a real mail: content
    // 6271px, frame 6527px, difference 256.
    //
    // documentElement stays as the fallback for the one case it was there
    // for — a body that reports nothing, which happens when everything in it
    // is floated or absolutely positioned.
    const h = d.body.scrollHeight || d.documentElement?.scrollHeight || 0
    if (h > 0) {
      // Slop so a rounding error cannot produce a scrollbar. Safe to add to
      // the body's height, which does not include it; adding it to the
      // documentElement's is what caused the drift above.
      height.value = h + 8
      ready.value = true
    }
  } catch {
    // Cross-origin refusal. Rather than clip the mail, give it room and let
    // the frame scroll internally — worse to read, but nothing is hidden.
    height.value = 800
    ready.value = true
  }
}

// A new mail in the same component: hide again and start measuring straight
// away, or the frame would show the incoming mail at the outgoing one's height
// — and would then wait for load to notice.
//
// nextTick, because the frame needs its new srcdoc before there is anything to
// read. The first tick or two may find no document yet; read() returns quietly
// in that case and the next one catches it.
watch(() => props.html, () => {
  height.value = 320
  ready.value = false
  nextTick(measure)
})

// The first mail is not a change, so the watcher above never fires for it.
onMounted(measure)
onBeforeUnmount(() => window.clearInterval(poll))
</script>

<style scoped>
.mail-frame {
  display: block;
  width: 100%;
  border: 0;
  /* No card and no border: the separation comes from the ground the mail
     shares with the pane, not from a box drawn around it. Set on the element
     too, so the frame's own rectangle is already the right colour before its
     document has painted anything. */
  background: var(--mail-ground);
  visibility: hidden;
}
.mail-frame.ready {
  visibility: visible;
}
</style>
