<template>
  <div v-loading="loading" class="customer-detail">
    <button class="back" type="button" @click="router.push('/basic/customers')">
      ← 返回客户列表
    </button>

    <section v-if="customer" class="hero">
      <div class="avatar">{{ customer.name.slice(0, 1).toUpperCase() }}</div>
      <div class="hero-main">
        <div class="eyebrow">{{ customer.code }}</div>
        <h1>{{ customer.name }}</h1>
        <div class="hero-tags">
          <el-tag>{{ displayCountry(customer.countryCode) }}</el-tag>
          <el-tag v-if="customer.customerType" type="info">{{
            optionLabel(types, customer.customerType)
          }}</el-tag>
          <el-tag :type="businessTag(customer.businessStatus)">{{
            businessLabel(customer.businessStatus)
          }}</el-tag>
          <el-tag :type="customer.status === 'ACTIVE' ? 'success' : 'info'">{{
            customer.status === "ACTIVE" ? "已启用" : "已停用"
          }}</el-tag>
        </div>
      </div>
      <div v-if="canWrite" class="hero-actions">
        <el-button @click="openBasic">编辑基础资料</el-button>
        <el-button type="primary" @click="openProfile">完善详细资料</el-button>
      </div>
    </section>

    <el-tabs
      v-if="customer"
      v-model="activeTab"
      class="detail-tabs"
      @tab-change="loadTab"
    >
      <el-tab-pane label="基本资料" name="basic">
        <div class="tab-stack">
          <section class="content-section">
            <SectionHead title="基础信息" />
            <div class="card-grid">
          <InfoCard title="客户概况" icon="企">
            <InfoRow label="客户简称" :value="customer.shortName" />
            <InfoRow label="英文名称" :value="customer.englishName" />
            <InfoRow
              label="客户类型"
              :value="optionLabel(types, customer.customerType)"
            />
            <InfoRow label="所属行业" :value="customer.industry" />
            <InfoRow
              label="客户来源"
              :value="optionLabel(sources, customer.source)"
            />
          </InfoCard>
          <InfoCard title="沟通信息" icon="联">
            <InfoRow label="官网" :value="customer.website" />
            <InfoRow label="主要语言" :value="customer.primaryLanguage" />
            <InfoRow label="所在时区" :value="customer.timezone" />
            <InfoRow label="标签" :value="(customer.tags || []).join('、')" />
            <InfoRow label="备注" :value="customer.remark" />
          </InfoCard>
            </div>
          </section>
        </div>
      </el-tab-pane>

      <el-tab-pane label="地址与税务" name="addresses">
        <div class="tab-stack">
          <section class="content-section">
            <SectionHead
              title="工商与税务资料"
              :action="canWrite ? '编辑税务资料' : ''"
              @action="openTax"
            />
        <div class="card-grid">
          <InfoCard title="注册资料" icon="证">
            <InfoRow label="注册名称" :value="customer.registeredName" />
            <InfoRow label="公司注册号" :value="customer.registrationNo" />
            <InfoRow label="税号" :value="customer.taxId" />
          </InfoCard>
          <InfoCard title="开票资料" icon="票">
            <InfoRow label="开票抬头" :value="customer.invoiceTitle" />
            <InfoRow label="开票税号" :value="customer.invoiceTaxNo" />
            <InfoRow label="开票备注" :value="customer.invoiceRemark" />
          </InfoCard>
        </div>
          </section>
          <section class="content-section">
        <SectionHead
          title="地址簿"
          :count="addresses.length"
          :action="canWrite ? '新增地址' : ''"
          @action="openAddress()"
        />
        <EmptyState v-if="!addresses.length" text="还没有维护地址" />
        <div v-else class="record-grid">
          <article
            v-for="item in addresses"
            :key="item.id"
            class="record-card"
            :class="{ inactive: item.status !== 'ACTIVE' }"
          >
            <div class="record-title">
              <strong>{{ addressTypeLabel(item.addressType) }}</strong
              ><el-tag v-if="item.isDefault" size="small" type="success"
                >默认</el-tag
              >
            </div>
            <p>
              {{
                [
                  displayCountry(item.countryCode),
                  item.state,
                  item.city,
                  item.postalCode,
                ]
                  .filter(Boolean)
                  .join(" · ")
              }}
            </p>
            <p class="record-main">{{ item.addressLine }}</p>
            <div
              v-if="canWrite && item.status === 'ACTIVE'"
              class="record-actions"
            >
              <el-button link type="primary" @click="openAddress(item)"
                >编辑</el-button
              ><el-button link type="danger" @click="removeAddress(item)"
                >停用</el-button
              >
            </div>
          </article>
        </div>
          </section>
        </div>
      </el-tab-pane>

      <el-tab-pane label="联系人" name="contacts">
        <div class="tab-stack">
          <section class="content-section">
        <SectionHead
          title="客户联系人"
          :count="contacts.length"
          :action="canWrite ? '新增联系人' : ''"
          @action="openContact()"
        />
        <EmptyState v-if="!contacts.length" text="还没有维护联系人" />
        <div v-else class="record-grid">
          <article
            v-for="item in contacts"
            :key="item.id"
            class="record-card"
            :class="{ inactive: item.status !== 'ACTIVE' }"
          >
            <div class="record-title">
              <strong>{{ item.name }}</strong
              ><el-tag v-if="item.isPrimary" size="small" type="success"
                >主要联系人</el-tag
              ><el-tag v-if="item.status !== 'ACTIVE'" size="small"
                >已停用</el-tag
              >
            </div>
            <p>
              {{
                [item.department, item.title].filter(Boolean).join(" · ") ||
                "未填写部门和职位"
              }}
            </p>
            <div class="contact-lines">
              <span>✉ {{ item.email || "—" }}</span
              ><span>☎ {{ item.phone || item.mobile || "—" }}</span
              ><span v-if="item.language">首选语言：{{ item.language }}</span>
            </div>
            <div class="mail-preferences">
              <el-tag
                size="small"
                :type="emailPermissionTag(item.emailPermission)"
                >{{ emailPermissionLabel(item.emailPermission) }}</el-tag
              ><el-tag
                v-for="category in item.emailCategories || []"
                :key="category"
                size="small"
                type="info"
                >{{ emailCategoryLabel(category) }}</el-tag
              ><span
                v-if="
                  item.emailPermission === 'ALLOWED' &&
                  !(item.emailCategories || []).length
                "
                >全部邮件类型</span
              >
            </div>
            <div
              v-if="canWrite && item.status === 'ACTIVE'"
              class="record-actions"
            >
              <el-button link type="primary" @click="openContact(item)"
                >编辑</el-button
              ><el-button link type="danger" @click="removeContact(item)"
                >停用</el-button
              >
            </div>
          </article>
        </div>
          </section>
        </div>
      </el-tab-pane>

      <el-tab-pane label="结算信用" name="credit">
        <div class="tab-stack">
          <section class="content-section">
        <SectionHead
          title="结算与信用"
          :action="canWrite ? '编辑结算信用' : ''"
          @action="openProfile"
        />
        <div class="credit-board">
          <div>
            <span>默认币种</span><strong>{{ customer.currency || "—" }}</strong>
          </div>
          <div>
            <span>付款方式</span
            ><strong>{{
              optionLabel(paymentOptions, customer.paymentTerm)
            }}</strong>
          </div>
          <div>
            <span>付款账期</span
            ><strong>{{
              customer.paymentDays
                ? `${customer.paymentDays} 天`
                : "现结 / 未设置"
            }}</strong>
          </div>
          <div>
            <span>信用额度</span
            ><strong>{{
              formatCredit(customer.creditLimitMinor, customer.creditCurrency)
            }}</strong>
          </div>
          <div>
            <span>信用状态</span
            ><el-tag :type="creditTag(customer.creditStatus)">{{
              creditLabel(customer.creditStatus)
            }}</el-tag>
          </div>
        </div>
          </section>

          <section class="content-section">
        <SectionHead title="信用评级" />
        <CreditRating
          v-if="customer"
          party="customers"
          :party-id="customer.id"
          :grade="customer.creditGrade || ''"
          :graded-at="customer.creditGradedAt || ''"
          :can-rate="canWrite"
          @rated="onRated"
        />
          </section>
        </div>
      </el-tab-pane>

      <el-tab-pane label="负责人" name="owners">
        <div class="tab-stack">
          <section class="content-section">
        <SectionHead
          title="内部负责人"
          :count="owners.length"
          :action="canWrite ? '添加负责人' : ''"
          @action="openOwner"
        />
        <el-alert
          type="info"
          :closable="false"
          title="一个客户可由多名员工按销售、跟单、单证、财务等职责共同负责。"
        />
        <EmptyState v-if="!owners.length" text="还没有关联负责人" />
        <div v-else class="owner-list">
          <div v-for="item in owners" :key="item.id" class="owner-row">
            <div class="owner-avatar">{{ item.employeeName.slice(0, 1) }}</div>
            <div class="owner-info">
              <div>
                <strong>{{ item.employeeName }}</strong
                ><el-tag v-if="item.isPrimary" size="small" type="success"
                  >主要负责人</el-tag
                >
              </div>
              <span>{{
                optionLabel(responsibilities, item.responsibilityCode)
              }}</span>
            </div>
            <div class="owner-date">
              {{ item.startDate || "立即生效"
              }}<span v-if="item.endDate"> 至 {{ item.endDate }}</span>
            </div>
            <div
              v-if="canWrite && item.status === 'ACTIVE'"
              class="owner-actions"
            >
              <el-button link type="primary" @click="openOwner(item)"
                >编辑</el-button
              >
              <el-button link type="danger" @click="removeOwner(item)"
                >移除</el-button
              >
            </div>
          </div>
        </div>
          </section>
        </div>
      </el-tab-pane>

      <el-tab-pane label="变更记录" name="changes">
        <div class="tab-stack">
          <section class="content-section">
        <SectionHead title="客户资料变更记录" :count="changeTotal" />
        <EmptyState v-if="!changes.length" text="还没有变更记录" />
        <el-timeline v-else class="history">
          <el-timeline-item
            v-for="item in changes"
            :key="item.id"
            :timestamp="formatTime(item.createdAt)"
            placement="top"
          >
            <div class="history-card">
              <strong>{{ item.summary }}</strong>
              <p>
                {{ sectionLabel(item.section) }} ·
                {{ item.operatorName || `员工 ${item.operatorId}` }}
              </p>
            </div>
          </el-timeline-item>
        </el-timeline>
          </section>
        </div>
      </el-tab-pane>
    </el-tabs>

    <el-dialog v-model="basicOpen" title="编辑基础资料" width="620px">
      <el-form :model="basicForm" label-width="100px">
        <el-form-item label="客户名称" required
          ><el-input v-model="basicForm.name"
        /></el-form-item>
        <el-form-item label="注册国家"
          ><el-select
            v-model="basicForm.countryCode"
            filterable
            clearable
            style="width: 100%"
            ><el-option
              v-for="c in countryChoices"
              :key="c.code"
              :label="c.name"
              :value="c.code" /></el-select
        ></el-form-item>
        <el-form-item label="默认币种"
          ><el-select v-model="basicForm.currency"
            ><el-option
              v-for="c in CURRENCIES"
              :key="c"
              :value="c" /></el-select
        ></el-form-item>
        <el-form-item label="付款方式"
          ><el-select v-model="basicForm.paymentTerm" clearable
            ><el-option
              v-for="o in paymentOptions"
              :key="o.code"
              :label="o.label"
              :value="o.code" /></el-select
        ></el-form-item>
        <el-form-item label="备注"
          ><el-input v-model="basicForm.remark" type="textarea" :rows="3"
        /></el-form-item> </el-form
      ><template #footer
        ><el-button @click="basicOpen = false">取消</el-button
        ><el-button type="primary" :loading="saving" @click="saveBasic"
          >保存</el-button
        ></template
      >
    </el-dialog>

    <el-dialog v-model="taxOpen" title="编辑税务资料" width="680px">
      <el-form :model="taxForm" label-width="110px" class="two-col-form">
        <el-form-item label="注册名称">
          <el-input v-model="taxForm.registeredName" />
        </el-form-item>
        <el-form-item label="公司注册号">
          <el-input v-model="taxForm.registrationNo" />
        </el-form-item>
        <el-form-item label="税号">
          <el-input v-model="taxForm.taxId" @blur="checkTaxDuplicates" />
        </el-form-item>
        <el-form-item label="开票抬头">
          <el-input v-model="taxForm.invoiceTitle" />
        </el-form-item>
        <el-form-item label="开票税号">
          <el-input v-model="taxForm.invoiceTaxNo" />
        </el-form-item>
        <el-form-item label="开票备注" class="full-row">
          <el-input v-model="taxForm.invoiceRemark" type="textarea" :rows="3" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="taxOpen = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="saveTax">
          保存
        </el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="profileOpen" title="完善客户详细资料" width="780px">
      <el-form :model="profileForm" label-width="110px" class="two-col-form">
        <el-form-item label="客户简称"
          ><el-input v-model="profileForm.shortName" /></el-form-item
        ><el-form-item label="英文名称"
          ><el-input v-model="profileForm.englishName"
        /></el-form-item>
        <el-form-item label="客户类型"
          ><el-select v-model="profileForm.customerType" clearable
            ><el-option
              v-for="o in types"
              :key="o.code"
              :label="o.label"
              :value="o.code" /></el-select></el-form-item
        ><el-form-item label="所属行业"
          ><el-input v-model="profileForm.industry"
        /></el-form-item>
        <el-form-item label="客户来源"
          ><el-select v-model="profileForm.source" clearable
            ><el-option
              v-for="o in sources"
              :key="o.code"
              :label="o.label"
              :value="o.code" /></el-select></el-form-item
        ><el-form-item label="业务状态"
          ><el-select v-model="profileForm.businessStatus"
            ><el-option label="潜在" value="PROSPECT" /><el-option
              label="合作中"
              value="COOPERATING" /><el-option
              label="暂停合作"
              value="PAUSED" /><el-option
              label="已停用"
              value="INACTIVE" /></el-select
        ></el-form-item>
        <el-form-item label="官网"
          ><el-input v-model="profileForm.website" /></el-form-item
        ><el-form-item label="主要语言"
          ><el-input v-model="profileForm.primaryLanguage"
        /></el-form-item>
        <el-form-item label="所在时区"
          ><el-input
            v-model="profileForm.timezone"
            placeholder="Asia/Shanghai" /></el-form-item
        ><el-form-item label="标签"
          ><el-select
            v-model="profileForm.tags"
            multiple
            allow-create
            filterable
            default-first-option
        /></el-form-item>
        <el-form-item label="注册名称"
          ><el-input v-model="profileForm.registeredName" /></el-form-item
        ><el-form-item label="公司注册号"
          ><el-input v-model="profileForm.registrationNo"
        /></el-form-item>
        <el-form-item label="税号"
          ><el-input
            v-model="profileForm.taxId"
            @blur="checkDuplicates" /></el-form-item
        ><el-form-item label="开票抬头"
          ><el-input v-model="profileForm.invoiceTitle"
        /></el-form-item>
        <el-form-item label="开票税号"
          ><el-input v-model="profileForm.invoiceTaxNo" /></el-form-item
        ><el-form-item label="付款账期"
          ><el-input-number v-model="profileForm.paymentDays" :min="0"
        /></el-form-item>
        <el-form-item label="信用额度"
          ><el-input-number
            v-model="profileForm.creditAmount"
            :min="0"
            :precision="2" /></el-form-item
        ><el-form-item label="额度币种"
          ><el-select v-model="profileForm.creditCurrency"
            ><el-option
              v-for="c in CURRENCIES"
              :key="c"
              :value="c" /></el-select
        ></el-form-item>
        <el-form-item label="信用状态"
          ><el-select v-model="profileForm.creditStatus"
            ><el-option label="正常" value="NORMAL" /><el-option
              label="关注"
              value="WATCH" /><el-option
              label="暂停赊销"
              value="CREDIT_SUSPENDED" /></el-select></el-form-item
        ><el-form-item label="开票备注"
          ><el-input v-model="profileForm.invoiceRemark"
        /></el-form-item> </el-form
      ><template #footer
        ><el-button @click="profileOpen = false">取消</el-button
        ><el-button type="primary" :loading="saving" @click="saveProfile"
          >保存</el-button
        ></template
      >
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
      :title="contactEditing ? '编辑联系人' : '新增联系人'"
      width="680px"
    >
      <el-form :model="contactForm" label-width="100px" class="two-col-form">
        <el-form-item label="姓名" required
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
      :title="ownerEditing ? '编辑客户负责人' : '添加客户负责人'"
      width="560px"
    >
      <el-form :model="ownerForm" label-width="110px">
        <el-form-item label="在职员工" required
          ><el-select
            v-model="ownerForm.employeeId"
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
import { computed, defineComponent, h, onMounted, reactive, ref } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import { useRoute, useRouter } from "vue-router";
import { del, get, post, put } from "../api";
import { CURRENCIES } from "../constants";
import { countryName, countryOptions } from "../lib/countries";
import {
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
const InfoCard = defineComponent({
  props: { title: String, icon: String },
  setup:
    (p, { slots }) =>
    () =>
      h("article", { class: "info-card" }, [
        h("header", { class: "info-card__header" }, [
          h("i", { class: "info-card__icon" }, p.icon),
          h("strong", { class: "info-card__title" }, p.title),
        ]),
        h("div", { class: "info-card__body" }, slots.default?.()),
      ]),
});
const EmptyState = defineComponent({
  props: { text: String },
  setup: (p) => () =>
    h("div", { class: "empty-state" }, [
      h("div", { class: "empty-icon" }, "＋"),
      h("p", p.text),
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
              { class: "section-head__action", onClick: () => emit("action") },
              p.action,
            )
          : null,
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
}
const loading = ref(true),
  saving = ref(false),
  activeTab = ref("basic");
const customer = ref<any>(null),
  addresses = ref<any[]>([]),
  contacts = ref<any[]>([]),
  owners = ref<any[]>([]),
  changes = ref<any[]>([]),
  changeTotal = ref(0);
const types = ref<any[]>([]),
  sources = ref<any[]>([]),
  paymentOptions = ref<any[]>([]),
  responsibilities = ref<any[]>([]),
  employees = ref<any[]>([]);
const countryChoices = computed(() => countryOptions("zh-CN"));
const basicOpen = ref(false),
  taxOpen = ref(false),
  profileOpen = ref(false),
  addressOpen = ref(false),
  contactOpen = ref(false),
  ownerOpen = ref(false);
const addressEditing = ref<string | null>(null),
  contactEditing = ref<string | null>(null),
  ownerEditing = ref<string | null>(null);
const basicForm = reactive<any>({}),
  taxForm = reactive<any>({}),
  profileForm = reactive<any>({}),
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
  return code ? countryName(code, "zh-CN") : "未设置国家";
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
  return v === "ALLOWED"
    ? "success"
    : v === "OPTED_OUT"
      ? "warning"
      : ("danger" as any);
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

async function loadCustomer() {
  const d = await get<any>(`/customers/${id}`);
  customer.value = d.customer;
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
async function loadTab(name: any) {
  if (name === "addresses") await loadAddresses();
  if (name === "contacts") await loadContacts();
  if (name === "owners") await loadOwners();
  if (name === "changes") await loadChanges();
}

function openBasic() {
  Object.assign(basicForm, {
    name: customer.value.name,
    countryCode: customer.value.countryCode,
    currency: customer.value.currency,
    paymentTerm: customer.value.paymentTerm,
    remark: customer.value.remark,
  });
  basicOpen.value = true;
}
async function saveBasic() {
  if (!basicForm.name) {
    ElMessage.warning("客户名称必填");
    return;
  }
  saving.value = true;
  try {
    await put(`/customers/${id}`, basicForm);
    basicOpen.value = false;
    await loadCustomer();
    ElMessage.success("基础资料已保存");
  } finally {
    saving.value = false;
  }
}
function openTax() {
  const c = customer.value;
  Object.assign(taxForm, {
    registeredName: c.registeredName,
    registrationNo: c.registrationNo,
    taxId: c.taxId,
    invoiceTitle: c.invoiceTitle,
    invoiceTaxNo: c.invoiceTaxNo,
    invoiceRemark: c.invoiceRemark,
  });
  taxOpen.value = true;
}
async function checkTaxDuplicates() {
  if (!taxForm.taxId) return;
  const d = await get<any>("/customers/duplicates", {
    name: customer.value.name,
    tax_id: taxForm.taxId,
    exclude_id: id,
  });
  if (d.candidates?.length) {
    ElMessage.warning(
      `发现相似客户：${d.candidates.map((x: any) => x.name).join("、")}`,
    );
  }
}
async function saveTax() {
  const c = customer.value;
  saving.value = true;
  try {
    await put(`/customers/${id}/profile`, {
      shortName: c.shortName,
      englishName: c.englishName,
      customerType: c.customerType,
      industry: c.industry,
      source: c.source,
      tags: c.tags || [],
      website: c.website,
      primaryLanguage: c.primaryLanguage,
      timezone: c.timezone,
      paymentDays: c.paymentDays || 0,
      creditLimitMinor: c.creditLimitMinor || 0,
      creditCurrency: c.creditCurrency || c.currency || "USD",
      creditStatus: c.creditStatus || "NORMAL",
      businessStatus: c.businessStatus || "PROSPECT",
      ...taxForm,
    });
    taxOpen.value = false;
    await loadCustomer();
    ElMessage.success("税务资料已保存");
  } finally {
    saving.value = false;
  }
}
function openProfile() {
  const c = customer.value;
  Object.assign(profileForm, {
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
    paymentDays: c.paymentDays || 0,
    creditAmount: Number(c.creditLimitMinor || 0) / 100,
    creditCurrency: c.creditCurrency || c.currency || "USD",
    creditStatus: c.creditStatus || "NORMAL",
    businessStatus: c.businessStatus || "PROSPECT",
  });
  profileOpen.value = true;
}
async function checkDuplicates() {
  if (!profileForm.taxId) return;
  const d = await get<any>("/customers/duplicates", {
    name: customer.value.name,
    tax_id: profileForm.taxId,
    exclude_id: id,
  });
  if (d.candidates?.length)
    ElMessage.warning(
      `发现相似客户：${d.candidates.map((x: any) => x.name).join("、")}`,
    );
}
async function saveProfile() {
  const error = validateCustomerProfile(profileForm);
  if (error) {
    ElMessage.warning(
      error === "websiteInvalid"
        ? "官网必须是以 http:// 或 https:// 开头的完整地址"
        : "请输入有效的 IANA 时区，例如 Asia/Shanghai",
    );
    return;
  }
  saving.value = true;
  try {
    const body = {
      ...profileForm,
      creditLimitMinor: Math.round(Number(profileForm.creditAmount || 0) * 100),
    };
    delete body.creditAmount;
    await put(`/customers/${id}/profile`, body);
    profileOpen.value = false;
    await loadCustomer();
    ElMessage.success("详细资料已保存");
  } finally {
    saving.value = false;
  }
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
    const body = { address: { ...addressForm } };
    addressEditing.value
      ? await put(`/customers/${id}/addresses/${addressEditing.value}`, body)
      : await post(`/customers/${id}/addresses`, body);
    addressOpen.value = false;
    await loadAddresses();
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
  await loadAddresses();
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
          nameRequired: "联系人姓名必填",
          emailInvalid: "联系人邮箱格式不正确",
          phoneInvalid: "电话或手机格式不正确",
        } as any
      )[error],
    );
    return;
  }
  saving.value = true;
  try {
    const body = { contact: { ...contactForm } };
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
          employeeId: "",
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
  if (!ownerForm.employeeId || !ownerForm.responsibilityCode) {
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
    ownerEditing.value
      ? await put(`/customers/${id}/owners/${ownerEditing.value}`, body)
      : await post(`/customers/${id}/owners`, body);
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
  await loadOwners();
}

onMounted(async () => {
  try {
    const [, , typeData, sourceData, paymentData, respData] = await Promise.all(
      [
        loadCustomer(),
        loadAddresses(),
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
.back {
  border: 0;
  background: transparent;
  color: var(--el-text-color-secondary);
  cursor: pointer;
  margin: 0 0 14px;
  padding: 0;
}
.hero {
  display: flex;
  align-items: center;
  gap: 18px;
  padding: 24px 28px;
  border: 1px solid #dce7f4;
  border-radius: 16px;
  background: linear-gradient(125deg, #f7fbff 0%, #fff 58%, #f7f9fc 100%);
  box-shadow: 0 8px 28px rgba(31, 78, 121, 0.06);
}
.avatar {
  width: 62px;
  height: 62px;
  border-radius: 16px;
  display: grid;
  place-items: center;
  color: #fff;
  background: linear-gradient(145deg, #2f80ed, #56ccf2);
  font-size: 26px;
  font-weight: 700;
}
.hero-main {
  flex: 1;
}
.eyebrow {
  font-size: 12px;
  letter-spacing: 0.12em;
  color: #6b7b8c;
}
.hero h1 {
  margin: 3px 0 9px;
  font-size: 26px;
}
.hero-tags {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.hero-actions {
  display: flex;
  gap: 10px;
}
.detail-tabs {
  margin-top: 18px;
  padding: 0 22px 26px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 14px;
  background: #fff;
}
.detail-tabs :deep(.el-tabs__header) {
  margin-bottom: 22px;
}
.tab-stack {
  display: flex;
  flex-direction: column;
  gap: 18px;
}
.content-section {
  padding: 20px;
  border: 1px solid #e4ebf3;
  border-radius: 12px;
  background: #fff;
}
.card-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}
.detail-tabs :deep(.info-card) {
  border: 1px solid #e4ebf3;
  border-radius: 12px;
  overflow: hidden;
}
.detail-tabs :deep(.info-card__header) {
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 14px 16px;
  background: #f8fafc;
}
.detail-tabs :deep(.info-card__icon) {
  width: 30px;
  height: 30px;
  border-radius: 9px;
  display: grid;
  place-items: center;
  background: #e8f3ff;
  color: #2474d2;
  font-style: normal;
}
.detail-tabs :deep(.info-card__title) {
  font-size: 15px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}
.detail-tabs :deep(.info-card__body) {
  padding: 8px 16px 12px;
}
.detail-tabs :deep(.info-row) {
  display: grid;
  grid-template-columns: 110px 1fr;
  gap: 14px;
  padding: 10px 0;
  border-bottom: 1px dashed #edf0f3;
}
.detail-tabs :deep(.info-row:last-child) {
  border: 0;
}
.detail-tabs :deep(.info-row__label) {
  color: var(--el-text-color-secondary);
}
.detail-tabs :deep(.info-row__value) {
  font-weight: 500;
  word-break: break-word;
}
.detail-tabs :deep(.section-head) {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin: 0 0 16px;
}
.detail-tabs :deep(.section-head__main) {
  display: flex;
  align-items: center;
  gap: 10px;
}
.detail-tabs :deep(.section-head__title) {
  margin: 0;
  font-size: 17px;
}
.detail-tabs :deep(.section-head__count) {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.detail-tabs :deep(.section-head__action) {
  border: 0;
  border-radius: 8px;
  padding: 8px 13px;
  background: var(--el-color-primary-light-9);
  color: var(--el-color-primary);
  cursor: pointer;
}
.record-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}
.record-card {
  position: relative;
  padding: 18px;
  border: 1px solid #e3e9f0;
  border-radius: 12px;
  background: #fff;
}
.record-card:hover {
  border-color: #b8d6fb;
  box-shadow: 0 7px 20px rgba(48, 103, 162, 0.08);
}
.record-card.inactive {
  opacity: 0.58;
  background: #fafafa;
}
.record-title {
  display: flex;
  align-items: center;
  gap: 8px;
}
.record-card p {
  margin: 9px 0 0;
  color: var(--el-text-color-secondary);
}
.record-card .record-main {
  color: var(--el-text-color-primary);
}
.record-actions {
  position: absolute;
  right: 12px;
  top: 9px;
}
.contact-lines {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 18px;
  margin-top: 12px;
  color: #536273;
}
.detail-tabs :deep(.empty-state) {
  display: grid;
  place-items: center;
  min-height: 170px;
  border: 1px dashed #d7e0ea;
  border-radius: 12px;
  color: var(--el-text-color-secondary);
}
.detail-tabs :deep(.empty-icon) {
  width: 48px;
  height: 48px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  background: #f1f6fc;
  color: #6e9ed4;
  font-size: 24px;
}
.credit-board {
  display: grid;
  grid-template-columns: repeat(5, 1fr);
  border: 1px solid #e3e9f0;
  border-radius: 14px;
  overflow: hidden;
}
.credit-board > div {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 22px;
  border-right: 1px solid #e3e9f0;
}
.credit-board > div:last-child {
  border: 0;
}
.credit-board span {
  font-size: 13px;
  color: var(--el-text-color-secondary);
}
.credit-board strong {
  font-size: 19px;
}
.owner-list {
  margin-top: 14px;
  border: 1px solid #e4eaf1;
  border-radius: 12px;
  overflow: hidden;
}
.owner-row {
  display: grid;
  grid-template-columns: 44px 1fr 220px 110px;
  align-items: center;
  gap: 12px;
  padding: 14px 16px;
  border-bottom: 1px solid #edf0f3;
}
.owner-row:last-child {
  border: 0;
}
.owner-avatar {
  width: 38px;
  height: 38px;
  border-radius: 11px;
  display: grid;
  place-items: center;
  background: #eaf4ff;
  color: #2676d2;
  font-weight: 700;
}
.owner-info {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.owner-info > div {
  display: flex;
  align-items: center;
  gap: 8px;
}
.owner-info span,
.owner-date {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.owner-actions {
  display: flex;
  justify-content: flex-end;
}
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
.history {
  padding-top: 8px;
}
.history-card {
  padding: 12px 16px;
  border: 1px solid #e4eaf1;
  border-radius: 10px;
}
.history-card p {
  margin: 6px 0 0;
  color: var(--el-text-color-secondary);
}
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
  .hero {
    align-items: flex-start;
    flex-wrap: wrap;
  }
  .hero-actions {
    width: 100%;
  }
  .content-section {
    padding: 16px;
  }
  .card-grid,
  .record-grid,
  .two-col-form {
    grid-template-columns: 1fr;
  }
  .credit-board {
    grid-template-columns: 1fr 1fr;
  }
  .owner-row {
    grid-template-columns: 44px 1fr;
  }
  .owner-date,
  .owner-actions {
    grid-column: 2;
  }
  .owner-actions {
    justify-content: flex-start;
  }
  .record-actions {
    position: static;
    margin-top: 10px;
  }
}
.two-col-form .full-row {
  grid-column: 1/-1;
}
.mail-preferences {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 10px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
@media (max-width: 900px) {
  .two-col-form .full-row {
    grid-column: auto;
  }
}
</style>
