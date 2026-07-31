<template>
  <div>
    <div class="page-head">
      <h2>{{ t('products.title') }}</h2>
      <div class="head-actions">
        <el-button v-if="canWrite" @click="categoryDialog = true">{{ t('products.categories') }}</el-button>
        <el-button v-if="canWrite" type="primary" @click="openCreate">{{ t('products.create') }}</el-button>
      </div>
    </div>

    <el-card shadow="never">
      <div class="filters">
        <el-input
          v-model="keyword"
          :placeholder="t('products.searchPlaceholder')"
          clearable
          style="width: 240px"
          @keyup.enter="reload"
          @clear="reload"
        />
        <el-select v-model="categoryId" :placeholder="t('products.allCategories')" clearable style="width: 180px" @change="reload">
          <el-option v-for="c in categories" :key="c.id" :value="c.id" :label="c.name" />
        </el-select>
        <el-button @click="reload">{{ t('common.query') }}</el-button>
        <el-checkbox v-model="showInactive" @change="reload">{{ t('products.showInactive') }}</el-checkbox>
      </div>

      <el-table :data="products" v-loading="loading">
        <el-table-column prop="code" :label="t('products.code')" width="110" />
        <el-table-column :label="t('products.name')" min-width="200">
          <template #default="{ row }">
            <div>{{ row.name }}</div>
            <div v-if="row.nameEn" class="sub">{{ row.nameEn }}</div>
          </template>
        </el-table-column>
        <el-table-column prop="categoryName" :label="t('products.category')" width="110" />
        <el-table-column :label="t('products.price')" width="120" align="right">
          <template #default="{ row }">
            <span v-if="row.referencePrice">{{ row.referencePrice }} {{ row.referenceCurrency }}</span>
            <span v-else class="sub">—</span>
          </template>
        </el-table-column>
        <el-table-column prop="baseUomCode" :label="t('products.uom')" width="70" />
        <el-table-column :label="t('products.hsCode')" width="120">
          <template #default="{ row }">{{ row.hsCode || '—' }}</template>
        </el-table-column>
        <el-table-column :label="t('common.status')" width="90">
          <template #default="{ row }">
            <el-tag :type="row.status === 'ACTIVE' ? 'success' : 'info'" size="small">
              {{ row.status === 'ACTIVE' ? t('common.active') : t('common.inactive') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column v-if="canWrite" :label="t('common.actions')" width="150" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openEdit(row)">{{ t('common.edit') }}</el-button>
            <el-button v-if="row.status === 'ACTIVE'" link type="danger" @click="deactivate(row)">
              {{ t('common.deactivate') }}
            </el-button>
            <el-button v-else link type="primary" @click="activate(row)">{{ t('common.activate') }}</el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-pagination
        class="pager"
        layout="total, prev, pager, next"
        :total="total"
        :page-size="pageSize"
        :current-page="page"
        @current-change="(p: number) => { page = p; load() }"
      />
    </el-card>

    <!-- ------------------------------------------------ product dialog -->
    <el-dialog
      v-model="dialogOpen"
      :title="editingId ? t('products.edit') : t('products.create')"
      width="720px"
    >
      <el-form :model="form" label-width="110px" v-loading="loadingDetail">
        <div class="grid">
          <el-form-item :label="t('products.code')">
            <el-input v-model="form.code" :disabled="!!editingId" :placeholder="t('products.codeAuto')" />
          </el-form-item>
          <el-form-item :label="t('products.type')">
            <el-select v-model="form.productType" style="width: 100%">
              <el-option v-for="ty in PRODUCT_TYPES" :key="ty" :value="ty" :label="t(`products.types.${ty}`)" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('products.name')" required>
            <el-input v-model="form.name" />
          </el-form-item>
          <el-form-item :label="t('products.nameEn')">
            <el-input v-model="form.nameEn" placeholder="Used on export documents" />
          </el-form-item>
          <el-form-item :label="t('products.category')" required>
            <el-select v-model="form.categoryId" filterable style="width: 100%" @change="loadAttrDefs">
              <el-option v-for="c in categories" :key="c.id" :value="c.id" :label="c.name" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('products.uom')" required>
            <el-select v-model="form.baseUomId" filterable style="width: 100%">
              <el-option v-for="u in uoms" :key="u.id" :value="u.id" :label="`${u.code} · ${u.name}`" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('products.price')">
            <el-input v-model="form.referencePrice" placeholder="0.00">
              <template #append>
                <el-select v-model="form.referenceCurrency" style="width: 90px">
                  <el-option value="USD" label="USD" />
                  <el-option value="EUR" label="EUR" />
                  <el-option value="CNY" label="CNY" />
                </el-select>
              </template>
            </el-input>
          </el-form-item>
          <el-form-item :label="t('products.brand')">
            <el-input v-model="form.brand" />
          </el-form-item>
        </div>

        <el-divider content-position="left">
          {{ t('products.taxSection') }}
          <span class="hint">{{ t('products.taxHint') }}</span>
        </el-divider>
        <div class="grid">
          <el-form-item :label="t('products.hsCode')">
            <el-input v-model="form.hsCode" placeholder="6302600000" />
          </el-form-item>
          <el-form-item :label="t('products.taxRate')">
            <el-input v-model="form.taxRate" placeholder="13">
              <template #append>%</template>
            </el-input>
          </el-form-item>
          <el-form-item :label="t('products.rebateRate')">
            <el-input v-model="form.exportRebateRate" placeholder="13">
              <template #append>%</template>
            </el-input>
          </el-form-item>
        </div>
        <el-form-item :label="t('products.description')">
          <el-input v-model="form.description" type="textarea" :rows="2" />
        </el-form-item>

        <!-- SKUs and files need an id, so they appear once the product exists. -->
        <template v-if="editingId">
          <el-divider content-position="left">
            {{ t('attrs.productAttrs') }}
            <span class="hint">{{ t('attrs.productAttrsHint') }}</span>
          </el-divider>
          <AttributeValueForm
            :defs="attrDefs"
            level="PRODUCT"
            :values="productAttrs"
            @update:values="(v) => (productAttrs = v)"
          />
          <div v-if="attrDefs.some((d) => d.level === 'PRODUCT')" class="sub-add">
            <el-button size="small" @click="saveProductAttrs">{{ t('attrs.saveAttrs') }}</el-button>
          </div>

          <el-divider content-position="left">
            {{ t('products.skus') }}
            <span class="hint">{{ t('products.skusHint') }}</span>
          </el-divider>
          <el-table :data="skus" size="small" class="sub-table">
            <el-table-column prop="code" :label="t('products.skuCode')" width="160" />
            <el-table-column prop="spec" :label="t('products.skuSpec')" min-width="180" />
            <el-table-column :label="t('common.status')" width="80">
              <template #default="{ row }">{{ row.status === 'ACTIVE' ? t('common.active') : t('common.inactive') }}</template>
            </el-table-column>
            <el-table-column width="140">
              <template #default="{ row }">
                <el-button link type="primary" @click="openSkuAttrs(row)">{{ t('attrs.attrs') }}</el-button>
                <el-button v-if="row.status === 'ACTIVE'" link type="danger" @click="removeSku(row)">
                  {{ t('common.deactivate') }}
                </el-button>
              </template>
            </el-table-column>
            <template #empty>{{ t('products.noSkus') }}</template>
          </el-table>
          <div class="sub-add">
            <el-input v-model="newSku.code" :placeholder="t('products.skuCode')" style="width: 160px" />
            <el-input v-model="newSku.spec" :placeholder="t('products.skuSpec')" style="width: 220px" />
            <el-button @click="addSku">{{ t('products.addSku') }}</el-button>
          </div>

          <el-divider content-position="left">
            {{ t('products.files') }}
            <span class="hint">{{ t('products.filesHint') }}</span>
          </el-divider>
          <el-table :data="attachments" size="small" class="sub-table">
            <el-table-column :label="t('products.fileName')" min-width="220">
              <template #default="{ row }">
                <a :href="row.downloadUrl" target="_blank" rel="noopener">{{ row.fileName }}</a>
              </template>
            </el-table-column>
            <el-table-column :label="t('products.fileSize')" width="100" align="right">
              <template #default="{ row }">{{ formatSize(row.fileSize) }}</template>
            </el-table-column>
            <el-table-column :label="t('products.uploadedAt')" width="140">
              <template #default="{ row }">{{ formatTime(row.uploadedAt) }}</template>
            </el-table-column>
            <el-table-column width="70">
              <template #default="{ row }">
                <el-button link type="danger" @click="removeFile(row)">{{ t('common.delete') }}</el-button>
              </template>
            </el-table-column>
            <template #empty>{{ t('products.noFiles') }}</template>
          </el-table>
          <el-upload
            class="sub-add"
            :show-file-list="false"
            :http-request="uploadFile"
            :disabled="uploading"
          >
            <el-button :loading="uploading">{{ t('products.upload') }}</el-button>
          </el-upload>
        </template>
      </el-form>
      <template #footer>
        <el-button @click="dialogOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="save">{{ t('common.save') }}</el-button>
      </template>
    </el-dialog>

    <!-- ------------------------------------------------ category dialog -->
    <el-dialog v-model="categoryDialog" :title="t('products.categories')" width="520px">
      <el-table :data="categories" size="small">
        <el-table-column prop="code" :label="t('products.categoryCode')" width="140" />
        <el-table-column prop="name" :label="t('products.categoryName')" min-width="160" />
        <el-table-column width="160">
          <template #default="{ row }">
            <el-button link type="primary" @click="openTemplate(row)">{{ t('attrs.template') }}</el-button>
            <el-button link type="danger" @click="removeCategory(row)">{{ t('common.deactivate') }}</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div class="sub-add">
        <el-input v-model="newCategory.code" :placeholder="t('products.categoryCode')" style="width: 140px" />
        <el-input v-model="newCategory.name" :placeholder="t('products.categoryName')" style="width: 180px" />
        <el-button @click="addCategory">{{ t('products.addCategory') }}</el-button>
      </div>
    </el-dialog>

    <AttributeTemplateEditor
      v-model:open="templateOpen"
      :category-id="templateCategory.id"
      :category-name="templateCategory.name"
      @saved="() => loadAttrDefs(form.categoryId)"
    />

    <el-dialog v-model="skuAttrOpen" :title="t('attrs.skuAttrs')" width="620px">
      <el-form-item :label="t('products.skuSpec')" label-width="70px">
        <el-input v-model="skuAttrSpec" />
      </el-form-item>
      <AttributeValueForm
        :defs="attrDefs"
        level="SKU"
        :values="skuAttrValues"
        @update:values="(v) => (skuAttrValues = v)"
      />
      <template #footer>
        <el-button @click="skuAttrOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" @click="saveSkuAttrs">{{ t('common.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { del, get, post, put } from '../api'
import { useAuthStore } from '../stores/auth'
import AttributeTemplateEditor from '../components/AttributeTemplateEditor.vue'
import AttributeValueForm from '../components/AttributeValueForm.vue'

interface Category { id: string; code: string; name: string; status: string }
interface Uom { id: string; code: string; name: string }
interface Product {
  id: string
  code: string
  name: string
  nameEn: string
  categoryId: string
  categoryName: string
  productType: string
  brand: string
  baseUomId: string
  baseUomCode: string
  referencePrice: string
  referenceCurrency: string
  hsCode: string
  taxRate: string
  exportRebateRate: string
  description: string
  status: string
}
interface Sku { id: string; code: string; spec: string; status: string; attributes?: string }
interface AttrDef {
  key: string
  label: string
  dataType: string
  unit: string
  enumValues?: string[]
  level: string
  isRequired: boolean
}
interface Attachment {
  id: string
  fileName: string
  fileSize: string
  uploadedAt: string
  downloadUrl: string
}

const PRODUCT_TYPES = ['FINISHED', 'SEMI', 'MATERIAL', 'SERVICE']
const EMPTY_FORM = {
  code: '', name: '', nameEn: '', categoryId: '', productType: 'FINISHED', brand: '',
  baseUomId: '', referencePrice: '', referenceCurrency: 'USD',
  hsCode: '', taxRate: '', exportRebateRate: '', description: '',
}

const { t } = useI18n()
const auth = useAuthStore()
const canWrite = auth.can('product:product:write')

const products = ref<Product[]>([])
const categories = ref<Category[]>([])
const uoms = ref<Uom[]>([])
const skus = ref<Sku[]>([])
const attachments = ref<Attachment[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 10
const keyword = ref('')
const categoryId = ref('')
const showInactive = ref(false)
const loading = ref(false)
const loadingDetail = ref(false)
const saving = ref(false)
const uploading = ref(false)
const dialogOpen = ref(false)
const categoryDialog = ref(false)
const editingId = ref<string | null>(null)
const form = reactive({ ...EMPTY_FORM })
const newSku = reactive({ code: '', spec: '' })
const newCategory = reactive({ code: '', name: '' })

// Attribute template of the product's category, resolved along the ancestry.
// Reloaded whenever the category changes, because the form is generated from it.
const attrDefs = ref<AttrDef[]>([])
const productAttrs = ref<Record<string, unknown>>({})
const templateOpen = ref(false)
const templateCategory = reactive({ id: '', name: '' })
const skuAttrOpen = ref(false)
const skuAttrEditing = ref<Sku | null>(null)
const skuAttrValues = ref<Record<string, unknown>>({})
const skuAttrSpec = ref('')

async function load() {
  loading.value = true
  try {
    const data = await get<{ products: Product[]; meta: { total: string } }>('/products', {
      page: page.value, page_size: pageSize, keyword: keyword.value,
      category_id: categoryId.value, status: showInactive.value ? 'ALL' : '',
    })
    products.value = data.products ?? []
    total.value = Number(data.meta.total)
  } finally {
    loading.value = false
  }
}

function reload() {
  page.value = 1
  load()
}

async function loadCategories() {
  categories.value = (await get<{ categories: Category[] }>('/product-categories', { status: 'ACTIVE' })).categories ?? []
}

function openCreate() {
  editingId.value = null
  skus.value = []
  attachments.value = []
  attrDefs.value = []
  productAttrs.value = {}
  Object.assign(form, EMPTY_FORM)
  dialogOpen.value = true
}

async function openEdit(row: Product) {
  editingId.value = row.id
  Object.assign(form, EMPTY_FORM)
  dialogOpen.value = true
  loadingDetail.value = true
  try {
    const data = await get<{ product: Product; skus: Sku[]; attachments: Attachment[] }>(`/products/${row.id}`)
    Object.assign(form, {
      code: data.product.code, name: data.product.name, nameEn: data.product.nameEn,
      categoryId: data.product.categoryId, productType: data.product.productType,
      brand: data.product.brand, baseUomId: data.product.baseUomId,
      referencePrice: data.product.referencePrice, referenceCurrency: data.product.referenceCurrency,
      hsCode: data.product.hsCode, taxRate: data.product.taxRate,
      exportRebateRate: data.product.exportRebateRate, description: data.product.description,
    })
    skus.value = data.skus ?? []
    attachments.value = data.attachments ?? []
    productAttrs.value = parseAttrs((data.product as unknown as { attributes?: string }).attributes)
    await loadAttrDefs(data.product.categoryId)
  } catch {
    dialogOpen.value = false
  } finally {
    loadingDetail.value = false
  }
}

// The form is generated from the template, so it has to be refetched
// whenever the category changes — a product moved from steel to textiles
// gets a different set of fields.
// Attributes cross the wire as a JSON string, so every read has to decode.
// A malformed value yields an empty form rather than breaking the dialog.
function parseAttrs(raw?: string): Record<string, unknown> {
  if (!raw) return {}
  try {
    const v = JSON.parse(raw)
    return v && typeof v === 'object' ? (v as Record<string, unknown>) : {}
  } catch {
    return {}
  }
}

async function loadAttrDefs(catId: string) {
  if (!catId) {
    attrDefs.value = []
    return
  }
  try {
    attrDefs.value = (await get<{ defs: AttrDef[] }>(`/categories/${catId}/attributes`)).defs ?? []
  } catch {
    attrDefs.value = []
  }
}

function openTemplate(row: Category) {
  templateCategory.id = row.id
  templateCategory.name = row.name
  templateOpen.value = true
}

// Attributes are saved separately from the product itself: they are validated
// against the template and can fail on their own, and a rejected attribute
// should not roll back a perfectly good name change.
async function saveProductAttrs() {
  if (!editingId.value) return
  await put(`/products/${editingId.value}/attributes`, {
    attributes_json: JSON.stringify(productAttrs.value),
  })
  ElMessage.success(t('attrs.saved'))
}

async function openSkuAttrs(row: Sku) {
  skuAttrEditing.value = row
  skuAttrSpec.value = row.spec
  const detail = await get<{ skus: Sku[] }>(`/products/${editingId.value}`)
  const fresh = (detail.skus ?? []).find((s) => s.id === row.id)
  skuAttrValues.value = parseAttrs(fresh?.attributes)
  skuAttrOpen.value = true
}

async function saveSkuAttrs() {
  if (!skuAttrEditing.value) return
  await put(`/skus/${skuAttrEditing.value.id}/attributes`, {
    attributes_json: JSON.stringify(skuAttrValues.value),
    spec: skuAttrSpec.value,
  })
  ElMessage.success(t('attrs.saved'))
  skuAttrOpen.value = false
  const detail = await get<{ skus: Sku[] }>(`/products/${editingId.value}`)
  skus.value = detail.skus ?? []
}

async function save() {
  if (!form.name || !form.categoryId || !form.baseUomId) {
    ElMessage.warning(t('products.required'))
    return
  }
  saving.value = true
  const body = {
    name: form.name, nameEn: form.nameEn, categoryId: form.categoryId,
    productType: form.productType, brand: form.brand, baseUomId: form.baseUomId,
    referencePrice: form.referencePrice, referenceCurrency: form.referenceCurrency,
    hsCode: form.hsCode, taxRate: form.taxRate, exportRebateRate: form.exportRebateRate,
    description: form.description,
  }
  try {
    if (editingId.value) {
      await put(`/products/${editingId.value}`, body)
      ElMessage.success(t('products.updated'))
    } else {
      await post('/products', { ...body, code: form.code })
      ElMessage.success(t('products.created'))
    }
    dialogOpen.value = false
    load()
  } finally {
    saving.value = false
  }
}

async function deactivate(row: Product) {
  await ElMessageBox.confirm(t('products.confirmDeactivate', { name: row.name }), t('products.confirmTitle'))
  await del(`/products/${row.id}`)
  ElMessage.success(t('products.deactivated'))
  load()
}

async function activate(row: Product) {
  await post(`/products/${row.id}/activate`)
  ElMessage.success(t('products.activated'))
  load()
}

// ---------------------------------------------------------------- skus

async function addSku() {
  if (!newSku.code) {
    ElMessage.warning(t('products.skuCodeRequired'))
    return
  }
  await post(`/products/${editingId.value}/skus`, { code: newSku.code, spec: newSku.spec })
  newSku.code = ''
  newSku.spec = ''
  skus.value = (await get<{ skus: Sku[] }>(`/products/${editingId.value}/skus`)).skus ?? []
}

async function removeSku(row: Sku) {
  await del(`/skus/${row.id}`)
  skus.value = (await get<{ skus: Sku[] }>(`/products/${editingId.value}/skus`)).skus ?? []
}

// ---------------------------------------------------------------- files

// Three steps on purpose: the bytes go straight from the browser to object
// storage, so a large file never occupies the gateway.
async function uploadFile(options: { file: File }) {
  const file = options.file
  uploading.value = true
  try {
    const presign = await post<{ fileKey: string; uploadUrl: string }>(
      `/products/${editingId.value}/attachments/presign`,
      { fileName: file.name, contentType: file.type || 'application/octet-stream' },
    )
    const put = await fetch(presign.uploadUrl, { method: 'PUT', body: file })
    if (!put.ok) {
      ElMessage.error(t('products.uploadFailed'))
      return
    }
    await post(`/products/${editingId.value}/attachments`, {
      fileKey: presign.fileKey, fileName: file.name,
      fileSize: String(file.size), contentType: file.type || 'application/octet-stream',
    })
    ElMessage.success(t('products.uploaded'))
    await refreshFiles()
  } finally {
    uploading.value = false
  }
}

async function removeFile(row: Attachment) {
  await del(`/attachments/${row.id}`)
  await refreshFiles()
}

async function refreshFiles() {
  attachments.value = (
    await get<{ attachments: Attachment[] }>(`/products/${editingId.value}/attachments`)
  ).attachments ?? []
}

// ---------------------------------------------------------------- categories

async function addCategory() {
  if (!newCategory.code || !newCategory.name) {
    ElMessage.warning(t('products.categoryRequired'))
    return
  }
  await post('/product-categories', { code: newCategory.code, name: newCategory.name })
  newCategory.code = ''
  newCategory.name = ''
  await loadCategories()
}

async function removeCategory(row: Category) {
  await del(`/product-categories/${row.id}`)
  await loadCategories()
}

function formatSize(bytes: string): string {
  const n = Number(bytes)
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

function formatTime(iso: string): string {
  return iso ? iso.replace('T', ' ').slice(0, 16) : ''
}

onMounted(async () => {
  load()
  await loadCategories()
  uoms.value = (await get<{ uoms: Uom[] }>('/uoms')).uoms ?? []
})
</script>

<style scoped>
.page-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}
.page-head h2 {
  font-size: 18px;
  font-weight: 500;
  margin: 0;
}
.head-actions {
  display: flex;
  gap: 10px;
}
.filters {
  display: flex;
  gap: 10px;
  margin-bottom: 14px;
  align-items: center;
}
.pager {
  margin-top: 14px;
  justify-content: flex-end;
}
.grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  column-gap: 12px;
}
.sub {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.hint {
  margin-left: 8px;
  font-weight: 400;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.sub-table {
  margin-bottom: 10px;
}
.sub-add {
  display: flex;
  gap: 8px;
  margin-bottom: 6px;
}
</style>
