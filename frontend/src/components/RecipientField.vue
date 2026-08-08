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
           Each chip says how many people it would add, not how many companies:
           twelve companies with nineteen contacts between them is a nineteen-
           message send, and the number somebody reads is the number they
           should be held to. -->
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
        <div class="chips">
          <!-- A toggle, not a fire-and-forget button. Clicking Brazil and then
               wondering whether it worked — or whether you clicked it twice —
               is the state this replaces: the chip now says whether that
               country is on the list, and clicking it again takes them off.
               aria-pressed rather than a colour alone, because "which ones did
               I pick" must not be a question only a sighted user can answer. -->
          <el-button
            v-for="g in countries"
            :key="g.code || 'none'"
            size="small"
            class="chip"
            :class="{ on: isCountryOn(g) }"
            :type="isCountryOn(g) ? 'primary' : ''"
            :aria-pressed="isCountryOn(g)"
            :loading="addingCountry === (g.code || 'none')"
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
          @keyup.enter="loadBook"
          @clear="loadBook"
        />
        <el-button @click="loadBook">{{ common('query') }}</el-button>
        <el-button link type="primary" @click="selectAllInBook">
          {{ t('emails.selectAll') }}
        </el-button>
      </div>
      <el-table
        ref="bookTable"
        :data="book"
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
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
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
    const d = await get<{ contacts: Recipient[] }>('/mailing-contacts', {
      keyword: bookKeyword.value,
    })
    book.value = d.contacts ?? []
    book.value.forEach((c) => known.set(c.email, c))
  } finally {
    loadingBook.value = false
  }
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

// Adds a whole country to the recipient list in one click.
//
// Goes through the same merge as the address book below, so picking Brazil and
// then ticking a Brazilian contact by hand does not send them two copies.
// Who a country's chip put on the list, remembered so the same click can take
// them off again. Keyed by country, filled the first time that chip is used.
const countryMembers = new Map<string, string[]>()

function chipKey(g: CountryGroup) {
  return g.code || 'none'
}

// A chip is on when everybody it stands for is currently on the list.
//
// Derived rather than stored. A flag would drift the moment somebody removed
// one of the tokens by hand: the chip would still look selected while the
// person it named was no longer being written to. Deriving it means the chip
// answers the only question worth asking — "is this whole country on the
// list" — and answers it correctly however the list got that way.
function isCountryOn(g: CountryGroup): boolean {
  const members = countryMembers.get(chipKey(g))
  if (!members || members.length === 0) return false
  const picked = new Set(props.modelValue.map((r) => r.email))
  return members.every((email) => picked.has(email))
}

async function toggleCountry(g: CountryGroup) {
  const where = g.code ? countryName(g.code, locale.value) : t('recipients.noCountry')
  if (isCountryOn(g)) {
    removeCountry(g, where)
    return
  }
  addingCountry.value = chipKey(g)
  try {
    const d = await get<{ contacts: Recipient[] }>('/mailing-contacts/by-country', {
      code: g.code,
      scope: countryScope.value,
    })
    const contacts = d.contacts ?? []
    countryMembers.set(chipKey(g), contacts.map((c) => c.email))
    const added = mergeIn(contacts)
    if (added === 0) {
      // Everybody was already on the list — by hand, or through another
      // route. The chip is now lit, which is the honest answer, and saying so
      // stops a click that changed nothing reading as a broken button.
      ElMessage.info(t('recipients.countryAllPresent', { country: where }))
      return
    }
    ElMessage.success(t('recipients.countryAdded', { n: added, country: where }))
  } finally {
    addingCountry.value = ''
  }
}

// Turning a country off means nobody from it is on the list — including
// anybody added by hand before the chip was pressed.
//
// The alternative is to remember which addresses this chip added and remove
// only those, which sounds more careful and is worse: the chip would go dark
// while two people from Brazil stayed on the list, and "Brazil is off" would
// be false. A chip that lies about the list it controls is not worth having.
function removeCountry(g: CountryGroup, where: string) {
  const members = new Set(countryMembers.get(chipKey(g)) ?? [])
  if (members.size === 0) return
  const kept = props.modelValue.filter((r) => !members.has(r.email))
  const removed = props.modelValue.length - kept.length
  emit('update:modelValue', kept)
  if (removed > 0) {
    ElMessage.info(t('recipients.countryRemoved', { n: removed, country: where }))
  }
}

// Switching between "every contact" and "one each" changes who a chip stands
// for, so what it remembers is no longer true. Cleared rather than refetched:
// the lists on screen are still whatever was picked, and silently swapping
// them under the person would be worse than making them press again.
watch(countryScope, () => countryMembers.clear())

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
.book-bar {
  display: flex;
  gap: 10px;
  margin-bottom: 12px;
}
.tagm {
  margin-left: 6px;
}
</style>
