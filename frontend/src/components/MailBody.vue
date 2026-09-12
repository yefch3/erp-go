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
import { embeddedAttachmentID } from '../lib/mailExcel'
import { safeExternalHref } from '../lib/frameLink'

const { t } = useI18n()
const props = defineProps<{ html: string }>()
const emit = defineEmits<{
  selectionContext: [payload: { text?: string; attachmentId?: string; x: number; y: number }]
  selectionClear: []
}>()

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
     worth imposing.

     height:auto only for images that state no height of their own. It used
     to be unconditional, and that broke every spacer: mail layouts stretch a
     10x10 transparent gif with width="520" height="12" into a divider, and
     auto threw the author's 12 away and let the square aspect ratio answer
     instead — 520px of blank, per spacer, a dozen times per newsletter. A
     stated height is layout, not a suggestion. */
  body{font-family:system-ui,-apple-system,"Segoe UI",Roboto,"Helvetica Neue",Arial,sans-serif;}
  img{max-width:100%;}
  img:not([height]){height:auto;}
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
  bindSelectionBubble()
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

// Selection events do not cross an iframe boundary. The parent owns these
// listeners (no script is admitted into the untrusted mail document), reads
// the user's current selection or the trusted attachment marker placed on an
// embedded image by the mail service, and emits coordinates in the app's
// viewport so the bubble can be rendered outside the frame.
//
// A bubble that follows the selection, not a replacement context menu. This
// used to intercept `contextmenu`, which cost the reader the browser's own
// right-click menu — copy, look up, translate — because a page cannot add an
// item to the native menu, only suppress it. Right-click now belongs entirely
// to the browser; our entry appears under the selection the moment one
// exists, and leaves with it.
// The parent-window mouseup listener of the current binding. Each new srcdoc
// is a new document and a new binding; the previous window listener must go
// with its document or they pile up one per opened mail.
let detachWindowMouseUp: (() => void) | null = null

function bindSelectionBubble() {
  const el = frame.value
  const d = el?.contentDocument
  if (!el || !d || d.documentElement.dataset.excelBubbleBound === '1') return
  d.documentElement.dataset.excelBubbleBound = '1'

  let timer = 0
  let dragging = false
  const evaluate = () => {
    const sel = d.getSelection()
    const text = sel?.toString().trim() ?? ''
    if (!sel || !text || sel.rangeCount === 0) {
      emit('selectionClear')
      return
    }
    const rect = sel.getRangeAt(0).getBoundingClientRect()
    if (rect.width === 0 && rect.height === 0) {
      emit('selectionClear')
      return
    }
    // The frame never scrolls internally (it is sized to its content), so
    // frame-viewport coordinates plus the frame's own position are already
    // app-viewport coordinates.
    const frameRect = el.getBoundingClientRect()
    emit('selectionContext', {
      text,
      x: frameRect.left + rect.left + rect.width / 2,
      y: frameRect.top + rect.bottom + 8,
    })
  }
  const schedule = (delay: number) => {
    window.clearTimeout(timer)
    timer = window.setTimeout(evaluate, delay)
  }

  // While the mouse is down the selection is still being made; judging it
  // then would flash the bubble across the screen as the drag grows.
  const finishDrag = () => {
    dragging = false
    // After the tick, not in it: on a plain click the selection collapses a
    // beat after mouseup, and reading it too early keeps a stale bubble open.
    schedule(0)
  }
  // 链接由**父窗口**去开，不指望 frame 自己能开。
  //
  // 文档里有 <base target="_blank">，sandbox 也给了 allow-popups，按规范这
  // 就够了——可线上就是点不动。这类行为在不同浏览器、不同 sandbox 组合下
  // 的差别很难复现，也很难保证以后不变，所以不赌它：拦下来交给父窗口，那
  // 边没有任何 sandbox 限制，是条确定的路。
  //
  // 顺带把协议白名单挪到了「点击那一刻」。净化器已经过滤过一遍，但净化和
  // 开链接是两件事——一个管存进来的 HTML，一个管我们要不要替使用者去打开
  // 这个地址。两道都设，谁也不替谁。见 lib/frameLink。
  d.addEventListener('click', (event) => {
    const target = event.target
    const anchor = target instanceof Element ? target.closest('a[href]') : null
    if (!anchor) return
    // 不管开不开得成，都不让 frame 自己去导航：开不成时原地不动，比把信
    // 换成一个打不开的页面好。
    event.preventDefault()
    const href = safeExternalHref(anchor.getAttribute('href'))
    if (!href) return
    // mailto 用一个临时锚点点一下，交给系统的邮件客户端；window.open 对它
    // 会先开一个空白页再关掉，闪一下。
    if (href.startsWith('mailto:')) {
      const a = document.createElement('a')
      a.href = href
      a.rel = 'noopener noreferrer'
      a.click()
      return
    }
    window.open(href, '_blank', 'noopener,noreferrer')
  })
  d.addEventListener('mousedown', () => {
    dragging = true
  })
  d.addEventListener('mouseup', finishDrag)
  // A drag that starts in the mail does not always end in it. A bottom-to-top
  // selection is usually released above the text — over the subject line, in
  // the parent document — where this frame's mouseup never fires; without
  // this, dragging stayed true and the bubble never appeared for backward
  // selections. The guard keeps parent clicks that never touched this frame
  // from being treated as the end of a drag.
  const windowUp = () => {
    if (dragging) finishDrag()
  }
  detachWindowMouseUp?.()
  window.addEventListener('mouseup', windowUp)
  detachWindowMouseUp = () => window.removeEventListener('mouseup', windowUp)
  // Keyboard selection (shift+arrows) never sees a mouseup.
  d.addEventListener('selectionchange', () => {
    if (!dragging) schedule(150)
  })
  // Hovering an existing selection brings the bubble back after a click in
  // the parent page dismissed it: that click closes the bubble but cannot
  // clear a selection that lives inside this frame.
  d.addEventListener('mouseover', (event) => {
    if (dragging) return
    const sel = d.getSelection()
    const text = sel?.toString().trim() ?? ''
    if (!sel || !text || sel.rangeCount === 0) return
    const rect = sel.getRangeAt(0).getBoundingClientRect()
    if (rect.width === 0 && rect.height === 0) return
    const mouse = event as MouseEvent
    if (mouse.clientX < rect.left || mouse.clientX > rect.right || mouse.clientY < rect.top || mouse.clientY > rect.bottom) return
    schedule(150)
  })

  // An embedded attachment image gets the same bubble on a plain click.
  // Linked images are left alone — the click is already spoken for (the
  // sandbox lets it open a popup), and fighting it would do both at once.
  d.addEventListener('click', (event) => {
    const target = event.target as { closest?: (selector: string) => Element | null } | null
    const image = target?.closest?.('img') as HTMLImageElement | null
    if (image?.closest('a[href]')) return
    const attachmentId = embeddedAttachmentID(image?.currentSrc || image?.getAttribute('src') || '')
    if (!attachmentId) return
    // The mouseup that preceded this click scheduled an evaluation that
    // would find no selection and close the bubble this click is opening.
    window.clearTimeout(timer)
    const frameRect = el.getBoundingClientRect()
    emit('selectionContext', {
      attachmentId,
      x: frameRect.left + event.clientX,
      y: frameRect.top + event.clientY + 8,
    })
  })
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
    // 量 <html> 这个盒子的实际高度，不是任何一个 scrollHeight。
    //
    // 三个候选值，各有各的毛病，实测（Chrome，把 frame 先后设成 120 和 400px）：
    //
    //   documentElement.scrollHeight  120 / 400 —— 跟着 frame 自己走
    //   body.scrollHeight              54 /  54 —— 稳，但少了 32px
    //   documentElement.rect.height    86 /  86 —— 稳，而且是对的
    //
    // 第一个是那次"读着读着信自己往下长"的原因：它至少等于 frame 的视口高度，
    // 而 frame 的高度是我们上次写进去的，于是每量一次都把上一次的答案再读回来
    // 加一遍 slop。三十二跳 × 8px = 256px 的漂移，真信上量到的就是这个数。
    //
    // 第二个是上一版的修法，它换来了另一个毛病：**每封信的末尾被切掉一截**。
    // body 的第一个和最后一个子元素的上下外边距会穿过 body 折叠出去（body 没有
    // 内边距和边框），而 body.scrollHeight 不含这截折叠出去的边距。一封 <p> 开头
    // <p> 结尾的信——也就是几乎所有纯文本信——因此少算 16 + 16 = 32px：正文整体
    // 被那 16px 顶下去，高度却没算，最后一行就被削掉半行。上面那封 86 对 54 的
    // demo 信就是这样，肉眼可见。
    //
    // 第三个既不跟着 frame 走（html 的高度是 auto，就是内容高），又包含折叠出去
    // 的边距。body.scrollHeight 留作下限：内容全是浮动或绝对定位时，html 盒子
    // 可能包不住它们。
    // 剩下的一种会漂的情况写在这儿，免得下次再查一遍：信自己的样式写了
    // html{height:100%} 时，<html> 的高度就是 frame 的高度，这三个数全都跟着
    // frame 走，于是每跳一次加 8px。它在改成量 rect 之前也一样会漂（那时是
    // body 跟着走），不是这次带来的；而且有界——poll 只跑三秒三十跳。真遇到
    // 一封这样的信，最多多出 240px 空白，不会没完没了。
    const rect = d.documentElement?.getBoundingClientRect().height ?? 0
    const h = Math.max(Math.ceil(rect), d.body.scrollHeight)
    if (h > 0) {
      // Slop so a rounding error cannot produce a scrollbar. 加在一个稳定的
      // 数上，所以它也只加一次。
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
onBeforeUnmount(() => {
  window.clearInterval(poll)
  detachWindowMouseUp?.()
})
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
