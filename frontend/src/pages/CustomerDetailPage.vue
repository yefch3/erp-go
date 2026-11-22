<template>
  <div v-loading="loading" class="customer-detail">
    <header v-if="customer" class="hero">
      <div class="hero-main">
        <h1>{{ displayName }} <small v-if="displayName !== customer.name">{{ customer.name }}</small></h1>
        <div class="hero-meta">
          <span class="hero-code">{{ customer.code }}</span>
          <span>{{ displayCountry(customer.countryCode) }}</span>
          <el-tag v-if="customer.customerType" size="small" type="info">
            {{ optionLabel(types, customer.customerType) }}
          </el-tag>
          <el-tag size="small" :type="businessTag(customer.businessStatus)">
            {{ businessLabel(customer.businessStatus) }}
          </el-tag>
          <el-tag size="small" :type="customer.status === 'ACTIVE' ? 'success' : 'info'">
            {{ customer.status === "ACTIVE" ? "已启用" : "已停用" }}
          </el-tag>
        </div>
      </div>
      <el-button class="hero-back" size="small" @click="router.push('/basic/customers')">
        ← 返回客户列表
      </el-button>
    </header>

    <nav v-if="customer" class="section-nav" aria-label="客户详情分区">
      <button
        v-for="section in sections"
        :key="section.key"
        type="button"
        :aria-current="currentSection === section.key ? 'location' : undefined"
        @click="navigateCustomerSection(section.key)"
      >
        {{ section.label }}
      </button>
    </nav>
    <div v-if="customer" class="detail-sections">
      <section v-for="section in sections" :id="'customer-' + section.key" :key="section.key" class="detail-section" tabindex="-1">
        <template v-if="section.key === 'basic'">
          <CustomerBasicInfo
            :ref="(el: any) => templateBasic = el"
            :customer-id="id"
            :can-write="canWrite"
            :busy="saving"
            @saving-change="basicSaving = $event"
            @updated="refreshAfterBasicSave"
          />
          <div class="content-block">
            <SectionHead title="其他资料" />
            <el-form v-if="canWrite" class="inline-edit" label-position="top" size="small" :disabled="saving || basicSaving" @submit.prevent="saveProfile">
              <div class="profile-fields">
                <el-form-item label="英文名称"><el-input v-model="profileForm.englishName" /></el-form-item>
                <el-form-item label="官网" :error="websiteInvalid ? '请填写完整的 http:// 或 https:// 地址' : ''"><el-input v-model="profileForm.website" @input="websiteInvalid = false" /></el-form-item>
                <el-form-item label="所属行业"><el-input v-model="profileForm.industry" /></el-form-item>
                <el-form-item label="主要语言"><el-input v-model="profileForm.primaryLanguage" /></el-form-item>
                <el-form-item label="标签">
                  <el-select v-model="profileForm.tags" multiple allow-create filterable default-first-option />
                </el-form-item>
                <el-form-item label="业务状态">
                  <el-select v-model="profileForm.businessStatus">
                    <el-option v-for="status in businessStatuses" :key="status" :label="businessLabel(status)" :value="status" />
                  </el-select>
                </el-form-item>
                <el-form-item label="备注" class="profile-remark"><el-input v-model="profileForm.remark" type="textarea" :autosize="{ minRows: 2 }" /></el-form-item>
              </div>
              <InlineActions v-if="profileDirty" :error="profileSaveError" :busy="saving || basicSaving" @cancel="cancelProfile" />
            </el-form>
            <div v-else class="profile-fields">
              <InfoRow label="英文名称" :value="customer.englishName" />
              <InfoRow label="官网" :value="customer.website" />
              <InfoRow label="所属行业" :value="customer.industry" />
              <InfoRow label="主要语言" :value="customer.primaryLanguage" />
              <InfoRow label="标签" :value="(customer.tags || []).join('、')" />
              <InfoRow label="业务状态" :value="businessLabel(customer.businessStatus)" />
              <InfoRow class="profile-remark" label="备注" :value="customer.remark" />
            </div>
          </div>
          <div v-if="visibleCustomFields.length" class="content-block">
            <SectionHead title="Excel 扩展信息" />
            <div v-for="field in visibleCustomFields" :key="field.fieldKey" class="custom-info-row">
              <span>{{ field.displayName }}</span>
              <strong>{{ field.value || '—' }}</strong>
              <el-button v-if="canWrite" link type="primary" @click="openCustomerField(field)">修改字段名</el-button>
            </div>
          </div>
        </template>

        <template v-if="section.key === 'addresses'">
          <SectionHead title="工商与税务资料" />
          <el-form v-if="canWrite" class="inline-edit" label-position="top" size="small" :disabled="saving || basicSaving" @submit.prevent="saveTax">
            <div class="profile-fields">
              <el-form-item label="注册名称"><el-input v-model="taxForm.registeredName" /></el-form-item>
              <el-form-item label="公司注册号"><el-input v-model="taxForm.registrationNo" /></el-form-item>
              <el-form-item label="税号"><el-input v-model="taxForm.taxId" @blur="checkTaxDuplicates" /></el-form-item>
            </div>
            <InlineActions v-if="taxDirty" :error="taxSaveError" :busy="saving || basicSaving" @cancel="cancelTax" />
          </el-form>
          <div v-else class="profile-fields">
            <InfoRow label="注册名称" :value="customer.registeredName" />
            <InfoRow label="公司注册号" :value="customer.registrationNo" />
            <InfoRow label="税号" :value="customer.taxId" />
          </div>

          <div class="content-block">
            <SectionHead title="地址簿" :count="addresses.length" :action="canWrite ? '新增地址' : ''" @action="openAddress()" />
            <p v-if="!addresses.length" class="empty-state">还没有维护地址</p>
            <div v-else class="record-list">
              <article v-for="item in addresses" :key="item.id" class="record-row">
                <div class="record-title">
                  <strong>{{ addressTypeLabel(item.addressType) }}</strong>
                  <el-tag v-if="item.isDefault" size="small" type="success">默认</el-tag>
                  <el-tag v-if="item.status !== 'ACTIVE'" size="small" type="info">已停用</el-tag>
                </div>
                <div class="record-detail">{{ item.addressLine || "—" }}</div>
                <div class="record-meta">{{ [item.countryCode && displayCountry(item.countryCode), item.state, item.city, item.postalCode].filter(Boolean).join(" · ") }}</div>
                <div v-if="canWrite && item.status === 'ACTIVE'" class="record-actions">
                  <el-button size="small" link type="primary" @click="openAddress(item)">编辑</el-button>
                  <el-button size="small" link type="danger" @click="removeAddress(item)">停用</el-button>
                </div>
              </article>
            </div>
          </div>
        </template>

        <template v-if="section.key === 'contacts'">
          <SectionHead title="客户联系人" :count="contacts.length" :action="canWrite ? '新增联系人' : ''" @action="openContact()" />
          <p v-if="!contacts.length" class="empty-state">还没有维护联系人</p>
          <div v-else class="record-list">
            <article v-for="item in contacts" :key="item.id" class="record-row">
              <div>
                <div class="record-title">
                  <strong>{{ item.name || "姓名待补充" }}</strong>
                  <el-tag v-if="item.isPrimary" size="small" type="success">主要联系人</el-tag>
                  <el-tag v-if="item.status !== 'ACTIVE'" size="small" type="info">已停用</el-tag>
                </div>
                <div v-if="item.department || item.title" class="record-meta record-note">
                  {{ [item.department, item.title].filter(Boolean).join(" · ") }}
                </div>
              </div>
              <div class="record-detail contact-methods">
                <div v-if="item.email || item.additionalEmails?.length">
                  <span v-if="item.email">{{ item.email }}</span>
                  <span v-for="email in item.additionalEmails || []" :key="email" class="record-meta" title="补充邮箱">{{ email }}</span>
                </div>
                <div v-if="item.phone || item.mobile">
                  <span v-if="item.phone">{{ item.phone }}</span>
                  <span v-if="item.mobile && item.mobile !== item.phone">{{ item.mobile }}</span>
                </div>
                <span v-if="!item.email && !item.additionalEmails?.length && !item.phone && !item.mobile" class="record-meta">—</span>
              </div>
              <div class="record-meta">
                <div class="mail-preferences">
                  <el-tag size="small" :type="emailPermissionTag(item.emailPermission)">{{ emailPermissionLabel(item.emailPermission) }}</el-tag>
                  <el-tag v-for="category in item.emailCategories || []" :key="category" size="small" type="info">{{ emailCategoryLabel(category) }}</el-tag>
                  <span v-if="(!item.emailPermission || item.emailPermission === 'ALLOWED') && !item.emailCategories?.length">全部邮件类型</span>
                </div>
                <div v-if="item.language" class="record-note">首选语言：{{ item.language }}</div>
              </div>
              <div v-if="canWrite && item.status === 'ACTIVE'" class="record-actions">
                <el-button size="small" link type="primary" @click="openContact(item)">编辑</el-button>
                <el-button size="small" link type="danger" @click="removeContact(item)">停用</el-button>
              </div>
            </article>
          </div>
        </template>

        <template v-if="section.key === 'owners'">
          <SectionHead title="内部负责人" :count="owners.length" :action="canWrite ? '添加负责人' : ''" @action="openOwner()" />
          <p v-if="!owners.length" class="empty-state">还没有关联负责人</p>
          <div v-else class="record-list">
            <article v-for="item in owners" :key="item.id" class="record-row">
              <div class="record-title">
                <strong>{{ item.employeeName }}</strong>
                <el-tag v-if="item.isPrimary" size="small" type="success">主要负责人</el-tag>
                <el-tag v-if="item.status !== 'ACTIVE'" size="small" type="info">已停用</el-tag>
              </div>
              <div class="record-detail">{{ optionLabel(responsibilities, item.responsibilityCode) }}</div>
              <div class="record-meta">
                {{ item.startDate || "立即生效" }}<span v-if="item.endDate"> 至 {{ item.endDate }}</span>
              </div>
              <div v-if="canWrite && item.status === 'ACTIVE'" class="record-actions">
                <el-button size="small" link type="primary" @click="openOwner(item)">编辑</el-button>
                <el-button size="small" link type="danger" @click="removeOwner(item)">移除</el-button>
              </div>
            </article>
          </div>
        </template>

        <template v-if="section.key === 'credit'">
          <SectionHead title="结算与信用" />
          <el-form v-if="canWrite" class="inline-edit" label-position="top" size="small" :disabled="saving || basicSaving" @submit.prevent="saveCredit">
            <div class="profile-fields">
              <el-form-item label="默认币种">
                <el-select v-model="creditForm.currency"><el-option v-for="currency in CURRENCIES" :key="currency" :value="currency" /></el-select>
              </el-form-item>
              <el-form-item label="付款方式">
                <el-select v-model="creditForm.paymentTerm" clearable>
                  <el-option v-for="option in paymentOptions" :key="option.code" :label="option.label" :value="option.code" />
                </el-select>
              </el-form-item>
              <el-form-item label="信用额度" :error="creditAmountError">
                <el-input-number v-model="creditForm.creditAmount" :min="0" :step="0.01" :precision="2" :controls="false" @change="creditAmountError = ''" />
              </el-form-item>
              <el-form-item label="额度币种">
                <el-select v-model="creditForm.creditCurrency"><el-option v-for="currency in CURRENCIES" :key="currency" :value="currency" /></el-select>
              </el-form-item>
              <el-form-item label="信用状态">
                <el-select v-model="creditForm.creditStatus">
                  <el-option label="正常" value="NORMAL" />
                  <el-option label="关注" value="WATCH" />
                  <el-option label="暂停赊销" value="CREDIT_SUSPENDED" />
                </el-select>
              </el-form-item>
            </div>
            <InlineActions v-if="creditDirty" :error="creditSaveError" :busy="saving || basicSaving" @cancel="cancelCredit" />
          </el-form>
          <div v-else class="profile-fields">
            <InfoRow label="默认币种" :value="customer.currency" />
            <InfoRow label="付款方式" :value="optionLabel(paymentOptions, customer.paymentTerm)" />
            <InfoRow label="信用额度" :value="formatCredit(customer.creditLimitMinor, customer.creditCurrency)" />
            <div class="info-row">
              <span class="info-row__label">信用状态</span>
              <el-tag size="small" :type="creditTag(customer.creditStatus)">{{ creditLabel(customer.creditStatus) }}</el-tag>
            </div>
          </div>

          <div class="content-block">
            <SectionHead title="信用评级" />
            <CreditRating
              party="customers"
              :party-id="customer.id"
              :grade="customer.creditGrade || ''"
              :graded-at="customer.creditGradedAt || ''"
              :can-rate="canWrite"
              compact
              @rated="onRated"
            />
          </div>
        </template>

        <template v-if="section.key === 'documents'">
          <SectionHead title="客户资料与附件" />
          <CustomerDocuments :customer-id="id" :can-write="canWrite" />
        </template>

        <template v-if="section.key === 'changes'">
          <SectionHead title="客户资料变更记录" :count="changeTotal" />
          <p v-if="!changes.length" class="empty-state">还没有变更记录</p>
          <el-timeline v-else class="history">
            <el-timeline-item v-for="item in changes" :key="item.id" :timestamp="formatTime(item.createdAt)" placement="top">
              <strong>{{ item.summary }}</strong>
              <p>{{ sectionLabel(item.section) }} · {{ item.operatorName || '员工 ' + item.operatorId }}</p>
            </el-timeline-item>
          </el-timeline>
        </template>
      </section>
    </div>

    <el-dialog v-model="customerFieldOpen" title="修改客户字段" width="560px">
      <el-alert type="info" :closable="false" title="修改显示名称会同步应用到所有客户，已保存的数据不会改变。" />
      <el-form :model="customerFieldForm" label-width="100px" style="margin-top:16px">
        <el-form-item label="显示名称" required><el-input v-model="customerFieldForm.displayName" /></el-form-item>
        <el-form-item label="Excel 别名"><el-select v-model="customerFieldForm.aliases" multiple allow-create filterable default-first-option style="width:100%" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="customerFieldOpen = false">取消</el-button><el-button type="primary" :loading="saving" @click="saveCustomerField">保存</el-button></template>
    </el-dialog>

    <el-dialog
      v-model="addressOpen"
      :title="addressEditing ? '编辑地址' : '新增地址'"
      width="620px"
    >
      <el-form :model="addressForm" label-width="100px"
        ><el-form-item label="地址类型" required
          ><el-select v-model="addressForm.addressType"
            ><el-option
              v-for="o in addressTypes"
              :key="o.value"
              :label="o.label"
              :value="o.value" /></el-select></el-form-item
        ><el-form-item label="国家/地区"
          ><el-select v-model="addressForm.countryCode" filterable clearable
            ><el-option
              v-for="c in countryChoices"
              :key="c.code"
              :label="c.name"
              :value="c.code" /></el-select></el-form-item
        ><el-form-item label="省/州与城市"
          ><div class="inline">
            <el-input
              v-model="addressForm.state"
              placeholder="省/州"
            /><el-input
              v-model="addressForm.city"
              placeholder="城市"
            /><el-input
              v-model="addressForm.postalCode"
              placeholder="邮编"
            /></div></el-form-item
        ><el-form-item label="详细地址" required
          ><el-input
            v-model="addressForm.addressLine"
            type="textarea"
            :rows="3" /></el-form-item
        ><el-form-item label="默认地址"
          ><el-switch v-model="addressForm.isDefault" /></el-form-item></el-form
      ><template #footer
        ><el-button @click="addressOpen = false">取消</el-button
        ><el-button type="primary" :loading="saving" @click="saveAddress"
          >保存</el-button
        ></template
      >
    </el-dialog>

    <el-dialog
      v-model="contactOpen"
      append-to-body
      top="5vh"
      :title="contactEditing ? '编辑联系人' : '新增联系人'"
      width="min(680px, 94vw)"
    >
      <el-form :model="contactForm" label-width="100px" class="two-col-form" style="max-height:68vh;overflow-y:auto">
        <el-form-item label="姓名" :required="!contactForm.email"
          ><el-input v-model="contactForm.name" /></el-form-item
        ><el-form-item label="主要联系人"
          ><el-switch v-model="contactForm.isPrimary"
        /></el-form-item>
        <el-form-item label="部门"
          ><el-input v-model="contactForm.department" /></el-form-item
        ><el-form-item label="职位"
          ><el-input v-model="contactForm.title"
        /></el-form-item>
        <el-form-item label="邮箱"
          ><el-input
            v-model="contactForm.email"
            placeholder="name@example.com" /></el-form-item
        ><el-form-item label="邮件接收"
          ><el-select v-model="contactForm.emailPermission"
            ><el-option label="允许接收" value="ALLOWED" /><el-option
              label="已退订"
              value="OPTED_OUT" /><el-option
              label="邮箱无效"
              value="INVALID" /></el-select
        ></el-form-item>
        <el-form-item label="电话"
          ><el-input v-model="contactForm.phone" /></el-form-item
        ><el-form-item label="手机"
          ><el-input v-model="contactForm.mobile"
        /></el-form-item>
        <el-form-item label="即时通讯"
          ><el-input
            v-model="contactForm.instantMessaging"
            placeholder="WhatsApp / 微信等" /></el-form-item
        ><el-form-item label="首选语言"
          ><el-select
            v-model="contactForm.language"
            clearable
            allow-create
            filterable
            ><el-option label="中文" value="zh-CN" /><el-option
              label="英语"
              value="en" /><el-option label="西班牙语" value="es" /></el-select
        ></el-form-item>
        <el-form-item label="邮件类型" class="full-row"
          ><el-select
            v-model="contactForm.emailCategories"
            multiple
            clearable
            placeholder="不选表示全部类型"
            ><el-option label="业务通知" value="BUSINESS" /><el-option
              label="报价/产品"
              value="QUOTATION" /><el-option
              label="船期/物流"
              value="SHIPPING" /><el-option
              label="营销活动"
              value="MARKETING" /></el-select
        ></el-form-item>
        <el-form-item label="备注" class="full-row"
          ><el-input v-model="contactForm.remark" type="textarea"
        /></el-form-item> </el-form
      ><template #footer
        ><el-button @click="contactOpen = false">取消</el-button
        ><el-button type="primary" :loading="saving" @click="saveContact"
          >保存</el-button
        ></template
      >
    </el-dialog>

    <el-dialog
      v-model="ownerOpen"
      append-to-body
      top="5vh"
      :title="ownerEditing ? '编辑客户负责人' : '添加客户负责人'"
      width="min(560px, 94vw)"
    >
      <el-form :model="ownerForm" label-width="110px">
        <el-form-item label="在职员工" required
          ><el-select
            v-model="ownerForm.employeeId"
            :key="ownerEditing || 'new-owner'"
            :multiple="!ownerEditing"
            :disabled="!!ownerEditing"
            filterable
            style="width: 100%"
            ><el-option
              v-for="e in employees"
              :key="e.id"
              :label="`${e.name} · ${e.departmentName || '未分部门'}`"
              :value="e.id"
          /></el-select>
          <div v-if="ownerEditing" class="form-tip">
            如需更换人员，请移除当前关系后重新添加，以保留清晰历史。
          </div></el-form-item
        >
        <el-form-item label="负责职责" required
          ><el-select v-model="ownerForm.responsibilityCode" style="width: 100%"
            ><el-option
              v-for="o in responsibilities"
              :key="o.code"
              :label="o.label"
              :value="o.code" /></el-select
        ></el-form-item>
        <el-form-item label="主要负责人"
          ><el-switch v-model="ownerForm.isPrimary" /><span class="switch-tip"
            >开启后会自动取消原主要负责人</span
          ></el-form-item
        >
        <el-form-item label="起止日期"
          ><el-date-picker
            v-model="ownerForm.dates"
            type="daterange"
            value-format="YYYY-MM-DD"
            start-placeholder="开始日期"
            end-placeholder="结束日期"
        /></el-form-item> </el-form
      ><template #footer
        ><el-button @click="ownerOpen = false">取消</el-button
        ><el-button type="primary" :loading="saving" @click="saveOwner"
          >保存</el-button
        ></template
      >
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, nextTick, onMounted, reactive, ref, watch, type Ref } from "vue";
import { ElButton, ElMessage, ElMessageBox } from "element-plus";
import { useRoute, useRouter } from "vue-router";
import { del, get, post, put, quietErrors, type Envelope } from "../api";
import CustomerBasicInfo from "../components/CustomerBasicInfo.vue";
import CustomerDocuments from "../components/CustomerDocuments.vue";
import { CURRENCIES } from "../constants";
import { countryName, countryOptions } from "../lib/countries";
import { isSystemCustomerFieldDefinition } from "../lib/customerImport";
import {
  customerAddressInput,
  customerContactInput,
  validateCustomerContact,
  validateCustomerProfile,
} from "../lib/customerForms";
import { useAuthStore } from "../stores/auth";
import CreditRating from "../components/CreditRating.vue";

const InfoRow = defineComponent({
  props: { label: String, value: [String, Number] },
  setup: (p) => () =>
    h("div", { class: "info-row" }, [
      h("span", { class: "info-row__label" }, p.label),
      h("strong", { class: "info-row__value" }, String(p.value || "—")),
    ]),
});
const SectionHead = defineComponent({
  props: { title: String, count: Number, action: String },
  emits: ["action"],
  setup:
    (p, { emit }) =>
    () =>
      h("div", { class: "section-head" }, [
        h("div", { class: "section-head__main" }, [
          h("h3", { class: "section-head__title" }, p.title),
          p.count !== undefined
            ? h("span", { class: "section-head__count" }, `${p.count} 条`)
            : null,
        ]),
        p.action
          ? h(
              "button",
              { class: "section-head__action", type: "button", onClick: () => emit("action") },
              p.action,
            )
          : null,
      ]),
});

const InlineActions = defineComponent({
  props: { busy: Boolean, error: String },
  emits: ["cancel"],
  setup: (p, { emit }) => () =>
    h("div", { class: "inline-actions" }, [
      p.error ? h("span", { class: "inline-error", role: "alert" }, p.error) : null,
      h(ElButton, { size: "small", disabled: p.busy, onClick: () => emit("cancel") }, () => "取消"),
      h(ElButton, { size: "small", type: "primary", nativeType: "submit", disabled: p.busy }, () => "保存修改"),
    ]),
});

const route = useRoute(),
  router = useRouter(),
  auth = useAuthStore(),
  id = String(route.params.id);
const canWrite = computed(() => auth.can("masterdata:customer:write"));

// 评完就地更新，不用刷整页——组件自己会重拉历史，这里只同步顶上那个当前
// 评级和「多久没评了」。
function onRated(grade: string) {
  if (!customer.value) return;
  customer.value.creditGrade = grade;
  customer.value.creditGradedAt = new Date().toISOString();
  void templateBasic.value?.load();
}
const loading = ref(true),
  saving = ref(false);
const customer = ref<any>(null),
  customerFieldDefinitions = ref<any[]>([]),
  addresses = ref<any[]>([]),
  contacts = ref<any[]>([]),
  owners = ref<any[]>([]),
  changes = ref<any[]>([]),
  changeTotal = ref(0);
const visibleCustomFields = computed(() => (customer.value?.customFields || []).filter((field: any) =>
  String(field.value ?? "").trim() && !isSystemCustomerFieldDefinition(field),
));
const types = ref<any[]>([]),
  sources = ref<any[]>([]),
  paymentOptions = ref<any[]>([]),
  responsibilities = ref<any[]>([]),
  employees = ref<any[]>([]);
const countryChoices = computed(() => countryOptions("zh-CN"));
const customerFieldOpen = ref(false),
  addressOpen = ref(false),
  contactOpen = ref(false),
  ownerOpen = ref(false);
const addressEditing = ref<string | null>(null),
  contactEditing = ref<string | null>(null),
  ownerEditing = ref<string | null>(null);
const customerFieldForm = reactive<any>({ fieldKey: "", displayName: "", aliases: [], sortOrder: 0 }),
  addressForm = reactive<any>({}),
  contactForm = reactive<any>({}),
  ownerForm = reactive<any>({});
const addressTypes = [
  { value: "REGISTERED", label: "注册地址" },
  { value: "OFFICE", label: "办公地址" },
  { value: "BILLING", label: "账单地址" },
  { value: "SHIPPING", label: "收货地址" },
];

function displayCountry(code: string) {
  return code ? countryName(code, "zh-CN") : "未分配国家";
}
function optionLabel(list: any[], code: string) {
  return list.find((o) => o.code === code)?.label || code || "—";
}
function businessLabel(v: string) {
  return (
    (
      {
        PROSPECT: "潜在",
        COOPERATING: "合作中",
        PAUSED: "暂停合作",
        INACTIVE: "已停用",
      } as any
    )[v] ||
    v ||
    "潜在"
  );
}
function businessTag(v: string) {
  return v === "COOPERATING"
    ? "success"
    : v === "PAUSED"
      ? "warning"
      : v === "INACTIVE"
        ? "info"
        : v === "PROSPECT"
          ? "primary"
          : ("info" as any);
}
function creditLabel(v: string) {
  return (
    ({ NORMAL: "正常", WATCH: "关注", CREDIT_SUSPENDED: "暂停赊销" } as any)[
      v
    ] ||
    v ||
    "正常"
  );
}
function creditTag(v: string) {
  return v === "NORMAL"
    ? "success"
    : v === "WATCH"
      ? "warning"
      : ("danger" as any);
}
function addressTypeLabel(v: string) {
  return addressTypes.find((o) => o.value === v)?.label || v;
}
function sectionLabel(v: string) {
  return (
    (
      {
        BASIC: "基础资料",
        PROFILE: "详细资料",
        ADDRESS: "地址",
        CONTACT: "联系人",
        OWNER: "负责人",
      } as any
    )[v] || v
  );
}
function emailPermissionLabel(v: string) {
  return (
    ({ ALLOWED: "允许接收", OPTED_OUT: "已退订", INVALID: "邮箱无效" } as any)[
      v
    ] || "允许接收"
  );
}
function emailPermissionTag(v: string) {
  if (v === "INVALID") return "danger";
  return v === "OPTED_OUT" ? "warning" : "success";
}
function emailCategoryLabel(v: string) {
  return (
    (
      {
        BUSINESS: "业务通知",
        QUOTATION: "报价/产品",
        SHIPPING: "船期/物流",
        MARKETING: "营销活动",
      } as any
    )[v] || v
  );
}
function formatCredit(v: any, c: string) {
  const n = Number(v || 0) / 100;
  return n
    ? `${c || "USD"} ${n.toLocaleString("zh-CN", { minimumFractionDigits: 2 })}`
    : "未设置";
}
function formatTime(v: string) {
  return v ? new Date(v).toLocaleString("zh-CN") : "—";
}

const templateBasic = ref<InstanceType<typeof CustomerBasicInfo> | null>(null);
async function refreshAfterBasicSave() {
  await Promise.all([loadCustomer(), loadAddresses(), loadChanges()]);
}
const displayName = computed(() => customer.value?.shortName?.trim() || customer.value?.name || "");
const sections = [
  { key: "basic", label: "基本资料" },
  { key: "addresses", label: "地址与税务" },
  { key: "contacts", label: "联系人" },
  { key: "owners", label: "负责人" },
  { key: "credit", label: "结算信用" },
  { key: "documents", label: "客户资料与附件" },
  { key: "changes", label: "变更记录" },
];
const currentSection = ref("basic");
async function navigateCustomerSection(section: unknown, behavior: ScrollBehavior = "smooth") {
  if (!sections.some((item) => item.key === section)) return;
  currentSection.value = String(section);
  await nextTick();
  const target = document.getElementById("customer-" + section);
  target?.scrollIntoView({ block: "start", behavior });
  target?.focus({ preventScroll: true });
}
watch(() => route.query.tab, (section) => void navigateCustomerSection(section));

async function loadCustomer() {
  const [d, fields] = await Promise.all([
    get<any>(`/customers/${id}`),
    get<any>("/customers/fields"),
  ]);
  customerFieldDefinitions.value = fields.fields || [];
  customer.value = d.customer;
  customer.value.customFields = (customer.value.customFields || []).map((field: any) => ({
    ...field,
    aliases: customerFieldDefinitions.value.find((definition: any) => definition.fieldKey === field.fieldKey)?.aliases || [],
  }));
}
async function loadAddresses() {
  addresses.value =
    (await get<any>(`/customers/${id}/addresses`, { status: "ALL" }))
      .addresses || [];
}
async function loadContacts() {
  contacts.value =
    (await get<any>(`/customers/${id}/contacts`, { status: "ALL" })).contacts ||
    [];
}
async function loadOwners() {
  owners.value =
    (await get<any>(`/customers/${id}/owners`, { status: "ALL" })).owners || [];
}
async function loadChanges() {
  const d = await get<any>(`/customers/${id}/changes`, {
    page: 1,
    page_size: 100,
  });
  changes.value = d.changes || [];
  changeTotal.value = Number(d.meta?.total || 0);
}
const basicSaving = ref(false);
const businessStatuses = ["PROSPECT", "COOPERATING", "PAUSED", "INACTIVE"];
const websiteInvalid = ref(false);
const creditAmountError = ref("");

function sectionDraft<T extends object>(read: () => T) {
  const baseline = computed(read);
  const form = ref(read()) as Ref<T>;
  const error = ref("");
  const fields = Object.keys(baseline.value) as (keyof T)[];
  const differs = (key: keyof T, saved: T) =>
    JSON.stringify(form.value[key] ?? "") !== JSON.stringify(saved[key] ?? "");
  const dirty = computed(() => fields.some((key) => differs(key, baseline.value)));
  watch(baseline, (next, previous) => {
    const updated = { ...next };
    for (const key of fields) {
      if (differs(key, previous)) updated[key] = form.value[key];
    }
    form.value = updated;
  });
  function reset() {
    form.value = { ...baseline.value };
    error.value = "";
  }
  return { form, baseline, dirty, error, reset };
}

const { form: profileForm, baseline: savedProfile, dirty: profileDirty, error: profileSaveError, reset: resetProfile } = sectionDraft(() => {
  const c = customer.value;
  return {
    englishName: c?.englishName || "",
    industry: c?.industry || "",
    website: c?.website || "",
    primaryLanguage: c?.primaryLanguage || "",
    tags: [...(c?.tags || [])] as string[],
    remark: c?.remark || "",
    businessStatus: c?.businessStatus || "PROSPECT",
  };
});
const { form: taxForm, baseline: savedTax, dirty: taxDirty, error: taxSaveError, reset: cancelTax } = sectionDraft(() => ({
  registeredName: customer.value?.registeredName || "",
  registrationNo: customer.value?.registrationNo || "",
  taxId: customer.value?.taxId || "",
}));
const { form: creditForm, baseline: savedCredit, dirty: creditDirty, error: creditSaveError, reset: resetCredit } = sectionDraft(() => {
  const c = customer.value;
  return {
    currency: c?.currency || "USD",
    paymentTerm: c?.paymentTerm || "",
    creditAmount: Number(c?.creditLimitMinor || 0) / 100,
    creditCurrency: c?.creditCurrency || c?.currency || "USD",
    creditStatus: c?.creditStatus || "NORMAL",
  };
});
function cancelProfile() {
  resetProfile();
  websiteInvalid.value = false;
}
function cancelCredit() {
  resetCredit();
  creditAmountError.value = "";
}

async function checkTaxDuplicates() {
  const taxId = taxForm.value.taxId.trim();
  if (!canWrite.value || !taxId || taxId === savedTax.value.taxId) return;
  const d = await get<any>("/customers/duplicates", {
    name: customer.value.name,
    tax_id: taxId,
    exclude_id: id,
  });
  if (taxForm.value.taxId.trim() === taxId && d.candidates?.length) {
    ElMessage.warning(`发现相似客户：${d.candidates.map((x: any) => x.name).join("、")}`);
  }
}
function profilePayload(overrides: Record<string, unknown> = {}) {
  const c = customer.value;
  return {
    shortName: c.shortName,
    englishName: c.englishName,
    customerType: c.customerType,
    industry: c.industry,
    source: c.source,
    tags: c.tags || [],
    website: c.website,
    primaryLanguage: c.primaryLanguage,
    timezone: c.timezone,
    registeredName: c.registeredName,
    registrationNo: c.registrationNo,
    taxId: c.taxId,
    invoiceTitle: c.invoiceTitle,
    invoiceTaxNo: c.invoiceTaxNo,
    invoiceRemark: c.invoiceRemark,
    creditLimitMinor: c.creditLimitMinor || 0,
    creditCurrency: c.creditCurrency || c.currency || "USD",
    creditStatus: c.creditStatus || "NORMAL",
    businessStatus: c.businessStatus || "PROSPECT",
    ...overrides,
  };
}
async function saveInlineSection(
  name: string,
  error: Ref<string>,
  reset: () => void,
  write: (saveLegacy: typeof writeLegacyCustomer) => Promise<void>,
) {
  if (!canWrite.value || saving.value || basicSaving.value) return;
  let legacySaved = false;
  saving.value = true;
  error.value = "";
  try {
    await write(async (overrides) => {
      await writeLegacyCustomer(overrides);
      legacySaved = true;
    });
    await loadCustomer();
    reset();
    ElMessage.success(name + "已保存");
    await loadChanges();
  } catch (err) {
    const failure = err as Partial<Envelope<unknown>> | undefined;
    const message = failure?.success === false && failure.message ? failure.message : "保存失败，请重试。";
    error.value = (legacySaved ? "部分资料已保存。" : "") + message;
  } finally {
    saving.value = false;
  }
}

async function writeLegacyCustomer(overrides: { currency?: string; paymentTerm?: string; remark?: string }) {
  const c = customer.value;
  await put(`/customers/${id}`, {
    name: c.name,
    countryCode: c.countryCode,
    country: c.country,
    address: c.address,
    currency: c.currency,
    paymentTerm: c.paymentTerm,
    remark: c.remark,
    ...overrides,
  }, quietErrors);
  customer.value = { ...customer.value, ...overrides };
}

async function saveTax() {
  if (!taxDirty.value) return;
  await saveInlineSection("税务资料", taxSaveError, cancelTax, async () => {
    await put(`/customers/${id}/profile`, profilePayload(taxForm.value), quietErrors);
  });
}

async function saveCredit() {
  if (!creditDirty.value) return;
  const { currency, paymentTerm, creditAmount, creditCurrency, creditStatus } = creditForm.value;
  const saved = savedCredit.value;
  const amountChanged = (creditAmount ?? 0) !== saved.creditAmount;
  const creditLimitMinor = Math.round((creditAmount || 0) * 100);
  if (amountChanged && (!Number.isSafeInteger(creditLimitMinor) || creditLimitMinor < 0)) {
    creditAmountError.value = "请输入有效的信用额度";
    return;
  }
  await saveInlineSection("结算信用", creditSaveError, cancelCredit, async (saveLegacy) => {
    if (currency !== saved.currency || (paymentTerm || "") !== saved.paymentTerm) {
      await saveLegacy({ currency, paymentTerm: paymentTerm || "" });
    }
    if (amountChanged || creditCurrency !== saved.creditCurrency || creditStatus !== saved.creditStatus) {
      await put(`/customers/${id}/profile`, profilePayload({
        creditLimitMinor: amountChanged ? String(creditLimitMinor) : customer.value.creditLimitMinor || 0,
        creditCurrency,
        creditStatus,
      }), quietErrors);
    }
  });
}
function openCustomerField(field: any) {
  Object.assign(customerFieldForm, {
    fieldKey: field.fieldKey,
    displayName: field.displayName,
    aliases: field.aliases || [field.displayName],
    sortOrder: field.sortOrder || 0,
  });
  customerFieldOpen.value = true;
}
async function saveCustomerField() {
  if (!String(customerFieldForm.displayName || "").trim()) {
    ElMessage.warning("请填写字段显示名称");
    return;
  }
  saving.value = true;
  try {
    await put(`/customers/fields/${encodeURIComponent(customerFieldForm.fieldKey)}`, {
      displayName: customerFieldForm.displayName,
      aliases: customerFieldForm.aliases,
      sortOrder: customerFieldForm.sortOrder,
    });
    customerFieldOpen.value = false;
    await loadCustomer();
    ElMessage.success("客户字段名称已更新");
  } finally {
    saving.value = false;
  }
}
async function saveProfile() {
  if (!profileDirty.value) return;
  const { remark, ...profile } = profileForm.value;
  websiteInvalid.value = validateCustomerProfile({ website: profile.website }) === "websiteInvalid";
  if (websiteInvalid.value) return;

  const { remark: savedRemark, ...saved } = savedProfile.value;
  await saveInlineSection("其他资料", profileSaveError, cancelProfile, async (saveLegacy) => {
    if (remark !== savedRemark) await saveLegacy({ remark });
    if (JSON.stringify(profile) !== JSON.stringify(saved)) {
      await put(`/customers/${id}/profile`, profilePayload(profile), quietErrors);
    }
  });
}

function openAddress(item?: any) {
  addressEditing.value = item?.id || null;
  Object.assign(
    addressForm,
    item || {
      addressType: "OFFICE",
      countryCode: customer.value.countryCode,
      state: "",
      city: "",
      postalCode: "",
      addressLine: "",
      isDefault: false,
      sortOrder: 0,
    },
  );
  addressOpen.value = true;
}
async function saveAddress() {
  if (!addressForm.addressLine) {
    ElMessage.warning("详细地址必填");
    return;
  }
  saving.value = true;
  try {
    // 只带接口认的字段：编辑时表单里还躺着列表行的 id、customerId、status，
    // 网关看到就整个拒收。
    const body = { address: customerAddressInput(addressForm) };
    addressEditing.value
      ? await put(`/customers/${id}/addresses/${addressEditing.value}`, body)
      : await post(`/customers/${id}/addresses`, body);
    addressOpen.value = false;
    await Promise.all([loadAddresses(), templateBasic.value?.load()]);
    ElMessage.success("地址已保存");
  } finally {
    saving.value = false;
  }
}
async function removeAddress(item: any) {
  await ElMessageBox.confirm(
    "停用后历史记录仍会保留，确定继续吗？",
    "停用地址",
  );
  await del(`/customers/${id}/addresses/${item.id}`);
  await Promise.all([loadAddresses(), templateBasic.value?.load()]);
}

function openContact(item?: any) {
  contactEditing.value = item?.id || null;
  Object.assign(
    contactForm,
    item
      ? {
          ...item,
          emailPermission: item.emailPermission || "ALLOWED",
          emailCategories: item.emailCategories || [],
        }
      : {
          name: "",
          department: "",
          title: "",
          email: "",
          phone: "",
          mobile: "",
          instantMessaging: "",
          language: "",
          remark: "",
          isPrimary: false,
          sortOrder: 0,
          emailPermission: "ALLOWED",
          emailCategories: [],
        },
  );
  contactOpen.value = true;
}
async function saveContact() {
  const error = validateCustomerContact(contactForm);
  if (error) {
    ElMessage.warning(
      (
        {
          nameRequired: "联系人姓名和邮箱至少填写一项",
          emailInvalid: "联系人邮箱格式不正确",
          phoneInvalid: "电话或手机格式不正确",
        } as any
      )[error],
    );
    return;
  }
  saving.value = true;
  try {
    // 同上：编辑时表单里有列表行的 id 和 status，不能原样发。
    const body = { contact: customerContactInput(contactForm) };
    contactEditing.value
      ? await put(`/customers/${id}/contacts/${contactEditing.value}`, body)
      : await post(`/customers/${id}/contacts`, body);
    contactOpen.value = false;
    await loadContacts();
    ElMessage.success("联系人已保存");
  } finally {
    saving.value = false;
  }
}
async function removeContact(item: any) {
  await ElMessageBox.confirm(
    "联系人不会被删除，只会停用并保留历史。",
    "停用联系人",
  );
  await del(`/customers/${id}/contacts/${item.id}`);
  await loadContacts();
}

async function openOwner(item?: any) {
  ownerEditing.value = item?.id || null;
  Object.assign(
    ownerForm,
    item
      ? {
          employeeId: item.employeeId,
          responsibilityCode: item.responsibilityCode,
          isPrimary: !!item.isPrimary,
          dates:
            item.startDate || item.endDate
              ? [item.startDate || "", item.endDate || ""]
              : [],
        }
      : {
          employeeId: [],
          responsibilityCode: "SALES",
          isPrimary: false,
          dates: [],
        },
  );
  if (!employees.value.length) {
    employees.value =
      (
        await get<any>("/employees", {
          page: 1,
          page_size: 200,
          employment_status: "ACTIVE",
        })
      ).employees || [];
  }
  ownerOpen.value = true;
}
async function saveOwner() {
  if (!ownerForm.employeeId || (Array.isArray(ownerForm.employeeId) && !ownerForm.employeeId.length) || !ownerForm.responsibilityCode) {
    ElMessage.warning("请选择员工和职责");
    return;
  }
  saving.value = true;
  try {
    const body = {
      owner: {
        employeeId: ownerForm.employeeId,
        responsibilityCode: ownerForm.responsibilityCode,
        startDate: ownerForm.dates?.[0] || "",
        endDate: ownerForm.dates?.[1] || "",
        isPrimary: !!ownerForm.isPrimary,
      },
    };
    if (ownerEditing.value) {
      await put(`/customers/${id}/owners/${ownerEditing.value}`, body);
    } else {
      const employeeIds = [...ownerForm.employeeId];
      for (const [index, employeeId] of employeeIds.entries()) {
        await post(`/customers/${id}/owners`, {
          owner: { ...body.owner, employeeId, isPrimary: body.owner.isPrimary && index === 0 },
        });
        // Keep only unsaved selections if a later request fails.
        ownerForm.employeeId = ownerForm.employeeId.filter((value: string) => value !== employeeId);
      }
    }
    ownerOpen.value = false;
    await loadOwners();
    ElMessage.success("负责人已保存");
  } finally {
    saving.value = false;
  }
}
async function removeOwner(item: any) {
  await ElMessageBox.confirm(
    `确定移除负责人“${item.employeeName}”吗？`,
    "移除负责人",
  );
  await del(`/customers/${id}/owners/${item.id}`);
  if (String(item.employeeId) === String(auth.employeeId)) {
    await router.push('/basic/customers');
    return;
  }
  await loadOwners();
}

onMounted(async () => {
  try {
    const [, , , , , typeData, sourceData, paymentData, respData] = await Promise.all(
      [
        loadCustomer(),
        loadAddresses(),
        loadContacts(),
        loadOwners(),
        loadChanges(),
        get<any>("/options", { category: "CUSTOMER_TYPE" }),
        get<any>("/options", { category: "CUSTOMER_SOURCE" }),
        get<any>("/options", { category: "PAYMENT_METHOD" }),
        get<any>("/options", { category: "CUSTOMER_OWNER_RESPONSIBILITY" }),
      ],
    );
    types.value = typeData.options || [];
    sources.value = sourceData.options || [];
    paymentOptions.value = paymentData.options || [];
    responsibilities.value = respData.options || [];
    await navigateCustomerSection(route.query.tab, "instant");
  } finally {
    loading.value = false;
  }
});
</script>

<style scoped>
.customer-detail {
  max-width: 1500px;
  margin: 0 auto;
}
.hero {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px 20px;
  padding: 14px 18px;
  color: #fff;
  background: linear-gradient(120deg, #143f5a, #176b87);
  border-radius: 10px;
}
.hero-main { min-width: 0; }
.hero h1 {
  margin: 0;
  font-size: 20px;
  line-height: 1.35;
  overflow-wrap: anywhere;
}
.hero h1 small {
  display: inline-block;
  margin-inline-start: 6px;
  color: #e5f2f7;
  font-size: 12px;
  font-weight: 400;
}
.hero-meta {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 4px 12px;
  margin-top: 7px;
  color: #e5f2f7;
  font-size: 13px;
  overflow-wrap: anywhere;
}
.hero-code { font-variant-numeric: tabular-nums; }
.hero-back { flex: none; }
.section-nav {
  position: sticky;
  top: 0;
  z-index: 2;
  display: flex;
  flex-wrap: wrap;
  gap: 2px 8px;
  margin-block: 8px;
  padding: 4px 8px;
  border: 1px solid #dce7f4;
  border-radius: 9px;
  background: #fff;
}
.section-nav button {
  padding: 6px 9px;
  font: inherit;
  font-size: 13px;
  color: #205573;
  background: transparent;
  border: 0;
  border-radius: 5px;
  cursor: pointer;
}
.section-nav button:hover,
.section-nav button:focus-visible,
.section-nav button[aria-current='location'] { background: #edf6fb; }
.section-nav button[aria-current='location'] { color: #135a7b; font-weight: 600; }
.detail-sections {
  padding-inline: 16px;
  border: 1px solid #e4ebf3;
  border-radius: 9px;
  background: #fff;
}
.detail-section {
  min-width: 0;
  padding: 11px 0;
  font-size: 13px;
  scroll-margin-top: 86px;
}
.detail-section + .detail-section { border-top: 1px solid #e4ebf3; }
.detail-section:focus { outline: none; }
.content-block { margin-top: 10px; }
.profile-fields { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 8px 16px; }
.profile-remark { grid-column: 1 / -1; white-space: pre-wrap; }
.inline-edit :deep(.el-form-item) { min-width: 0; margin-bottom: 4px; }
.inline-edit :deep(.el-form-item__label) { margin-bottom: 2px; color: #526578; }
.inline-edit :deep(.el-select), .inline-edit :deep(.el-input-number) { width: 100%; }
.inline-edit :deep(.el-input-number input) { text-align: left; }
.inline-edit :deep(.el-form-item.is-error) { margin-bottom: 16px; }
.detail-sections :deep(.inline-actions) { display: flex; justify-content: flex-end; align-items: center; flex-wrap: wrap; gap: 8px; margin-top: 6px; }
.detail-sections :deep(.inline-actions .el-button + .el-button) { margin-left: 0; }
.detail-sections :deep(.inline-error) { margin-inline-end: auto; color: var(--el-color-danger-dark-2); font-size: 12px; }
.info-row, .detail-sections :deep(.info-row) { display: grid; align-content: start; justify-items: start; gap: 4px; min-width: 0; }
.info-row__label, .detail-sections :deep(.info-row__label) { color: #526578; font-size: 12px; }
.detail-sections :deep(.info-row__value) { font-size: 13px; font-weight: 500; overflow-wrap: anywhere; }
.custom-info-row { display: grid; grid-template-columns: minmax(90px, .7fr) minmax(0, 1fr) auto; align-items: center; gap: 12px; padding: 7px 0; border-bottom: 1px solid #edf0f3; }
.custom-info-row:last-child { border-bottom: 0; }
.custom-info-row > span { color: var(--el-text-color-secondary); font-size: 13px; }
.custom-info-row > strong { font-weight: 500; overflow-wrap: anywhere; }
.detail-sections :deep(.section-head) { display: flex; align-items: center; justify-content: space-between; gap: 8px; margin: 0 0 10px; }
.detail-sections :deep(.section-head__main) { display: flex; align-items: center; gap: 8px; }
.detail-sections :deep(.section-head__title) { margin: 0; color: #173b52; font-size: 14px; }
.detail-sections :deep(.section-head__count) { color: var(--el-text-color-secondary); font-size: 12px; }
.detail-sections :deep(.section-head__action) {
  flex: none;
  padding: 3px 0;
  border: 0;
  background: transparent;
  color: #12638b;
  font: inherit;
  font-size: 13px;
  cursor: pointer;
}
.detail-sections :deep(.section-head__action:hover) { text-decoration: underline; }
.record-list {
  border-top: 1px solid #edf0f3;
}
.record-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1.5fr) minmax(0, 1fr) 82px;
  align-items: start;
  gap: 4px 16px;
  padding-block: 7px;
  line-height: 20px;
}
.record-row + .record-row { border-top: 1px solid #edf0f3; }
.record-title { display: flex; align-items: center; flex-wrap: wrap; gap: 4px 6px; min-width: 0; }
.record-title strong { overflow-wrap: anywhere; }
.record-detail, .record-meta { min-width: 0; overflow-wrap: anywhere; }
.record-detail { white-space: pre-wrap; }
.record-meta { color: var(--el-text-color-secondary); font-size: 12px; }
.record-note { margin-top: 2px; }
.record-actions { display: flex; justify-content: flex-end; align-items: center; }
.record-actions .el-button + .el-button { margin-left: 8px; }
.contact-methods > div { display: flex; flex-wrap: wrap; gap: 1px 12px; }
.contact-methods span { min-width: 0; }
.empty-state { margin: 0; color: var(--el-text-color-secondary); font-size: 13px; }
.form-tip {
  margin-top: 5px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
  line-height: 1.4;
}
.switch-tip {
  margin-left: 10px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.history { margin: 0; padding: 5px 0 0 5px; }
.history strong { overflow-wrap: anywhere; }
.history p { margin: 4px 0 0; color: var(--el-text-color-secondary); font-size: 13px; }
.two-col-form {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0 18px;
}
.two-col-form :deep(.el-select),
.two-col-form :deep(.el-input-number) {
  width: 100%;
}
.inline {
  display: grid;
  grid-template-columns: 1fr 1fr 110px;
  gap: 8px;
  width: 100%;
}
@media (max-width: 900px) {
  .profile-fields { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .two-col-form { grid-template-columns: 1fr; }
}
@media (max-width: 540px) {
  .hero { flex-wrap: wrap; }
  .hero h1 small { display: block; margin-inline-start: 0; }
  .profile-fields { grid-template-columns: minmax(0, 1fr); }
  .detail-section { scroll-margin-top: 112px; }
  .record-row { grid-template-columns: minmax(0, 1fr) 82px; gap: 3px 8px; }
  .record-detail, .record-meta { grid-column: 1; }
  .record-actions { grid-column: 2; grid-row: 1; }
}
.two-col-form .full-row {
  grid-column: 1/-1;
}
.mail-preferences { display: flex; align-items: center; flex-wrap: wrap; gap: 3px 6px; }
@media (max-width: 900px) {
  .two-col-form .full-row {
    grid-column: auto;
  }
}
</style>
