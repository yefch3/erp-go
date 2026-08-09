<template>
  <div class="recip-field">
    <!-- One field, three ways in: type an address nobody has recorded, type a
         prefix and pick a suggestion, or open the address book and tick.
         Gmail's To field works this way and people expect it to. -->
    <el-select
      v-model="picked"
      multiple
      filterable
      remote
      allow-create
      default-first-option
      :reserve-keyword="false"
      :remote-method="search"
      :loading="searching"
      :placeholder="t('recipients.placeholder')"
      class="picker"
      @change="onChange"
    >
      <el-option
        v-for="c in suggestions"
        :key="c.email"
        :label="c.email"
        :value="c.email"
      >
        <div class="opt">
          <span class="opt-name">{{ c.name }}</span>
          <span class="opt-mail">{{ c.email }}</span>
          <span class="opt-co">{{ c.customerName }}</span>
        </div>
      </el-option>
      <!-- Free-form entries carry no contact record, so nothing resolves the
           {{contact_name}} variable for them. Said here rather than at send
           time, when it is too late to fix cheaply. -->
      <template #empty>
        <div class="empty">{{ t('recipients.typeToSearch') }}</div>
      </template>
    </el-select>

    <div class="bar">
      <el-button size="small" @click="bookOpen = true">
        {{ t('recipients.fromBook') }}
      </el-button>
      <span class="count">{{ t('emails.selectedCount', { n: modelValue.length }) }}</span>
      <span v-if="unknownCount > 0" class="warn">
        {{ t('recipients.unknownWarning', { n: unknownCount }) }}
      </span>
      <span class="grow" />
      <el-button v-if="modelValue.length" size="small" link @click="clearAll">
        {{ t('emails.clearSelection') }}
      </el-button>
    </div>

    <el-dialog v-model="bookOpen" :title="t('recipients.book')" width="760px" append-to-body>
      <!-- Countries first, because "everyone in Brazil" is a different act
           from "find Klaus" and the two should not be typed into the same box.
           Each chip says how many people it stands for, not how many
           companies: twelve companies with nineteen contacts between them is a
           nineteen-message send, and the number somebody reads is the number
           they should be held to.

           A chip filters the table rather than adding straight to the field.
           It used to add nineteen people on one click and announce it in a
           toast, which meant the only way to find out who they were was to
           read nineteen tokens afterwards — and by then it had happened.
           Now the country's people appear in the list below, already ticked,
           and nothing is committed until 添加所选. Seeing the names before
           agreeing to write to them is the whole point. -->
      <div v-if="countries.length" class="country-strip">
        <div class="strip-head">
          <span class="strip-title">{{ t('recipients.byCountry') }}</span>
          <!-- Whole company first, because that is the default and the order
               of the two buttons is itself a statement about which one is
               ordinary. -->
          <el-radio-group v-model="countryScope" size="small">
            <el-radio-button value="all">{{ t('recipients.scopeAll') }}</el-radio-button>
            <el-radio-button value="primary">{{ t('recipients.scopePrimary') }}</el-radio-button>
          </el-radio-group>
          <span class="strip-hint">{{ t(`recipients.scopeHint_${countryScope}`) }}</span>
        </div>
        <div v-if="pickedCountries.size" class="strip-foot">
          {{ t('recipients.countryFilterOn', { n: book.length }) }}
          <el-button link type="primary" size="small" @click="clearCountryFilter">
            {{ t('recipients.countryFilterClear') }}
          </el-button>
        </div>
        <div class="chips">
          <!-- A toggle, not a fire-and-forget button. Clicking Brazil and then
               wondering whether it worked — or whether you clicked it twice —
               is the state this replaces: the chip now says whether that
               country is on the list, and clicking it again takes them off.
               aria-pressed rather than a colour alone, because "which ones did
               I pick" must not be a question only a sighted user can answer. -->
          <el-button
            v-for="g in countries"
            :key="chipKey(g)"
            size="small"
            class="chip"
            :class="{ on: isCountryOn(g) }"
            :type="isCountryOn(g) ? 'primary' : ''"
            :aria-pressed="isCountryOn(g)"
            :loading="addingCountry === chipKey(g)"
            :disabled="countAdded(g) === 0 && !isCountryOn(g)"
            @click="toggleCountry(g)"
          >
            <span v-if="isCountryOn(g)" class="tick" aria-hidden="true">✓</span>
            {{ g.code ? countryName(g.code, locale) : t('recipients.noCountry') }}
            <span class="chip-n">{{ countAdded(g) }}</span>
          </el-button>
        </div>
      </div>

      <div class="book-bar">
        <el-input
          v-model="bookKeyword"
          :placeholder="t('emails.searchContacts')"
          clearable
          style="width: 260px"
          @keyup.enter="searchFromBar"
          @clear="searchFromBar"
        />
        <el-button @click="searchFromBar">{{ common('query') }}</el-button>
        <el-button link type="primary" @click="selectAllInBook">
          {{ t('emails.selectAll') }}
        </el-button>
      </div>
      <!-- row-key, because the rows are now replaced under the table whenever a
           country filter changes. Without it el-table tracks rows by position:
           filtering five contacts down to three left Diego rendering Ana's
           "primary" tag, because he had landed on the row she used to occupy.
           An address is unique in this list and is the natural identity. -->
      <el-table
        ref="bookTable"
        :data="book"
        row-key="email"
        height="320"
        size="small"
        v-loading="loadingBook"
        @selection-change="onBookSelection"
      >
        <el-table-column type="selection" width="42" />
        <el-table-column :label="t('emails.contact')" min-width="130">
          <template #default="{ row }">
            <span>{{ row.name }}</span>
            <el-tag v-if="row.isPrimary" size="small" effect="plain" class="tagm">
              {{ t('emails.primary') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="email" :label="t('emails.email')" min-width="190" />
        <el-table-column prop="customerName" :label="t('emails.customer')" min-width="170" />
        <el-table-column :label="t('emails.country')" width="110">
          <template #default="{ row }">
            {{ row.countryCode ? countryName(row.countryCode, locale) : row.country }}
          </template>
        </el-table-column>
      </el-table>
      <template #footer>
        <el-button @click="bookOpen = false">{{ common('cancel') }}</el-button>
        <el-button type="primary" @click="addFromBook">
          {{ t('recipients.addChosen', { n: bookChosen.length }) }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { get } from '../api'
import { countryName } from '../lib/countries'

export interface Recipient {
  contactId?: string
  name: string
  email: string
  customerId?: string
  customerName: string
  country?: string
  countryCode?: string
}

/** One country and how big a send to it would be. */
interface CountryGroup {
  code: string
  customerCount: string
  contactCount: string
  oneEachCount: string
}

const props = defineProps<{ modelValue: Recipient[] }>()
const emit = defineEmits<{ 'update:modelValue': [Recipient[]] }>()

const { t, locale } = useI18n()
const common = (k: string) => t(`common.${k}`)

const picked = ref<string[]>([])
const suggestions = ref<Recipient[]>([])
const searching = ref(false)
const bookOpen = ref(false)
const book = ref<Recipient[]>([])
const bookKeyword = ref('')
const loadingBook = ref(false)
const bookChosen = ref<Recipient[]>([])
const bookTable = ref()
const countries = ref<CountryGroup[]>([])
// The whole company by default.
//
// The first version defaulted to one contact per customer, reasoning that
// over-sending is the mistake you apologise for. That reads the wrong way
// round for this trade: the counterparty here is a *company*, the buyer, the
// shipping clerk and the boss all expect to be on the thread, and copying the
// team is ordinary rather than intrusive. The expensive mistake is the quiet
// one — the price update that never reached the person who decides.
//
// Both are still real intentions, so the choice stays; only which one you get
// without thinking about it has changed.
const countryScope = ref<'primary' | 'all'>('all')
const addingCountry = ref('')

// How many people this chip would actually add, which is not the same as how
// many companies are in the country.
function countAdded(g: CountryGroup): number {
  return Number(countryScope.value === 'all' ? g.contactCount : g.oneEachCount)
}

// Everything ever seen, keyed by address, so a token keeps its contact
// details after the suggestion list has moved on to another query.
const known = new Map<string, Recipient>()

// An address typed by hand resolves no contact name, so {{contact_name}}
// would land in the needs-attention queue at send time. Warning now costs a
// glance; finding out later costs a re-send.
const unknownCount = computed(() => props.modelValue.filter((r) => !r.name).length)

onMounted(() => {
  syncFromModel()
  void runSearch('')
})

watch(() => props.modelValue, syncFromModel, { deep: true })

function syncFromModel() {
  props.modelValue.forEach((r) => known.set(r.email, r))
  const next = props.modelValue.map((r) => r.email)
  if (next.join('\u0000') !== picked.value.join('\u0000')) picked.value = next
}

// Trigram indexes need three characters to be selective, and el-select fires
// its remote method on every keystroke — typing "klaus" would otherwise be
// five round trips and five scans. Debounce plus a floor makes it one.
const MIN_QUERY = 2
const DEBOUNCE_MS = 250
let searchTimer: ReturnType<typeof setTimeout> | undefined
// Guards against an earlier, slower response overwriting a later one.
let searchSeq = 0

function search(query: string) {
  const q = query.trim()
  if (searchTimer) clearTimeout(searchTimer)
  // An empty box lists the book; one stray character is not worth a query.
  if (q !== '' && q.length < MIN_QUERY) {
    searching.value = false
    return
  }
  searching.value = true
  searchTimer = setTimeout(() => void runSearch(q), DEBOUNCE_MS)
}

async function runSearch(q: string) {
  const mine = ++searchSeq
  try {
    const d = await get<{ contacts: Recipient[] }>('/mailing-contacts', { keyword: q })
    if (mine !== searchSeq) return
    suggestions.value = d.contacts ?? []
    suggestions.value.forEach((c) => known.set(c.email, c))
  } finally {
    if (mine === searchSeq) searching.value = false
  }
}

onBeforeUnmount(() => {
  if (searchTimer) clearTimeout(searchTimer)
})

// el-select hands back plain strings — both the ones picked from suggestions
// and the ones typed freehand. Anything we have a contact for keeps its
// details; the rest become a bare address, which is exactly what it is.
function onChange(emails: string[]) {
  emit(
    'update:modelValue',
    emails.map(
      (e) =>
        known.get(e) ?? { name: '', email: e.trim().toLowerCase(), customerName: '' },
    ),
  )
}

function clearAll() {
  picked.value = []
  emit('update:modelValue', [])
}

async function loadBook() {
  loadingBook.value = true
  try {
    book.value = pickedCountries.value.size ? await countryContacts() : await searchContacts()
    book.value.forEach((c) => known.set(c.email, c))
    // Everything a country filter brought in starts ticked, so "everyone in
    // Brazil" is still one decision — but a visible one, with names attached
    // and an untick next to each. Rows the person is already writing to are
    // ticked too: leaving them clear would read as "these will be dropped".
    await nextTick()
    const already = new Set(props.modelValue.map((r) => r.email))
    book.value.forEach((c) => {
      if (pickedCountries.value.size || already.has(c.email)) {
        bookTable.value?.toggleRowSelection(c, true)
      }
    })
  } finally {
    loadingBook.value = false
  }
}

// The two ways of finding somebody, kept apart. Searching is for "find Klaus";
// the chips are for "everyone in Brazil". Mixing them would need a rule for
// what a keyword means inside a country filter, and every answer to that is a
// surprise to somebody.
async function searchContacts(): Promise<Recipient[]> {
  const d = await get<{ contacts: Recipient[] }>('/mailing-contacts', {
    keyword: bookKeyword.value,
  })
  return d.contacts ?? []
}

// One request per selected country, merged and de-duplicated. A contact
// belongs to one customer and a customer to one country, so overlap is not
// expected — the de-duplication is here because "not expected" is not the same
// as "cannot happen", and a doubled row would be ticked twice and sent twice.
async function countryContacts(): Promise<Recipient[]> {
  const lists = await Promise.all(
    [...pickedCountries.value].map((key) =>
      get<{ contacts: Recipient[] }>('/mailing-contacts/by-country', {
        code: key === 'none' ? '' : key,
        scope: countryScope.value,
      }).then((d) => d.contacts ?? []),
    ),
  )
  const seen = new Set<string>()
  const out: Recipient[] = []
  lists.flat().forEach((c) => {
    if (seen.has(c.email)) return
    seen.add(c.email)
    out.push(c)
  })
  return out
}

watch(bookOpen, (open) => {
  if (!open) return
  loadBook()
  void loadCountries()
})

async function loadCountries() {
  const d = await get<{ countries: CountryGroup[] }>('/customer-countries')
  countries.value = d.countries ?? []
}

// A country chip filters the list below; it does not send anybody anything.
//
// Selecting one puts that country's people in the table, already ticked, and
// nothing leaves the dialog until 添加所选. The earlier version added them to
// the field on the spot and said so in a toast — which is the wrong shape for
// a decision this size: nineteen people arrived as nineteen tokens and the
// only way to check who they were was to read them all back afterwards.
const pickedCountries = ref<Set<string>>(new Set())

function chipKey(g: CountryGroup) {
  return g.code || 'none'
}

function isCountryOn(g: CountryGroup): boolean {
  return pickedCountries.value.has(chipKey(g))
}

async function toggleCountry(g: CountryGroup) {
  const key = chipKey(g)
  const next = new Set(pickedCountries.value)
  if (next.has(key)) {
    next.delete(key)
  } else {
    next.add(key)
  }
  pickedCountries.value = next
  // Deliberately silent. A toast on every press was noise on an action whose
  // whole result is visible in the table a centimetre below it.
  addingCountry.value = key
  try {
    await loadBook()
  } finally {
    addingCountry.value = ''
  }
}

// Searching drops the country filter rather than searching inside it. Both
// are ways of finding people and holding both at once needs a rule nobody
// asked for; dropping it is at least visible, because the chips go dark.
function searchFromBar() {
  pickedCountries.value = new Set()
  void loadBook()
}

function clearCountryFilter() {
  pickedCountries.value = new Set()
  void loadBook()
}

// Changing what a chip stands for changes who is in the table, so the list is
// rebuilt. The ticks go with it — they described the old membership.
watch(countryScope, () => {
  if (pickedCountries.value.size) void loadBook()
})

function onBookSelection(rows: Recipient[]) {
  bookChosen.value = rows
}

function selectAllInBook() {
  book.value.forEach((r) => bookTable.value?.toggleRowSelection(r, true))
}

// Adds rather than replaces, and de-duplicates: picking the same contact
// twice from two searches should not send them two copies. Shared by the
// address book and the country chips so both obey the same rule.
function mergeIn(incoming: Recipient[]): number {
  const merged = [...props.modelValue]
  const seen = new Set(merged.map((r) => r.email))
  let added = 0
  incoming.forEach((c) => {
    if (seen.has(c.email)) return
    seen.add(c.email)
    merged.push(c)
    known.set(c.email, c)
    added++
  })
  if (added > 0) emit('update:modelValue', merged)
  return added
}

function addFromBook() {
  mergeIn(bookChosen.value)
  bookOpen.value = false
}
</script>

<style scoped>
.recip-field {
  width: 100%;
}
.picker {
  width: 100%;
}
.opt {
  display: flex;
  align-items: baseline;
  gap: 10px;
}
.opt-name {
  font-weight: 500;
}
.opt-mail,
.opt-co {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.empty {
  padding: 10px 20px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.bar {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 8px;
}
.grow {
  flex: 1;
}
.count {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.warn {
  font-size: 12px;
  color: var(--el-color-warning);
}
.country-strip {
  margin-bottom: 12px;
  padding: 10px 12px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 6px;
  background: var(--el-fill-color-lighter);
}
.strip-head {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 8px;
}
.strip-title {
  font-size: 13px;
  font-weight: 600;
}
.strip-hint {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.chips {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.chip-n {
  margin-left: 5px;
  padding: 0 5px;
  border-radius: 8px;
  background: var(--el-color-primary-light-8);
  color: var(--el-color-primary);
  font-size: 11px;
}
/* On a lit chip the count sits on a filled background, so the light tint it
   uses when the chip is plain would disappear into it. */
.chip.on .chip-n {
  background: rgba(255, 255, 255, 0.25);
  color: #fff;
}
.tick {
  margin-right: 3px;
  font-weight: 700;
}
.strip-foot {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 8px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.book-bar {
  display: flex;
  gap: 10px;
  margin-bottom: 12px;
}
.tagm {
  margin-left: 6px;
}
</style>
