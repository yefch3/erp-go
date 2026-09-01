<template>
  <div class="page">
    <header class="page-head">
      <div>
        <div class="eyebrow">{{ t('bankTransactions.eyebrow') }}</div>
        <h1>{{ t('bankTransactions.title') }}</h1>
        <p>{{ t('bankTransactions.subtitle') }}</p>
      </div>
      <div class="head-actions">
        <!-- 导入用的兜底币种，不是筛选条件——所以它贴着导入按钮，并且
             把「什么时候会用到」写在提示里：有人把它当成查询框问过。 -->
        <el-tooltip :content="t('bankTransactions.defaultCurrencyHint')" placement="bottom">
          <el-select
            v-model="defaultCurrency"
            clearable
            style="width: 172px"
            :placeholder="t('bankTransactions.defaultCurrency')"
          >
            <el-option v-for="c in CURRENCIES" :key="c" :value="c" :label="c" />
          </el-select>
        </el-tooltip>
        <el-button v-if="canWrite" @click="openRecord">{{ t('bankTransactions.record') }}</el-button>
        <!-- 我们自己的账户。原来住在收款对账页上，那页撤掉之后搬来这里——
             账户是「钱进了我们哪个户头」，本来就是这本账的一部分。
             按 export:receipt:read 显示：接口挂的是这个权限，采购经理能打开
             本页但没有它，不判断的话按钮点下去就是 403。 -->
        <el-button v-if="canReadAccounts" @click="openAccounts">{{ t('bankTransactions.accounts') }}</el-button>
        <el-button v-if="canWrite" type="primary" :loading="importing" @click="fileInput?.click()">
          {{ t('bankTransactions.import') }}
        </el-button>
        <input ref="fileInput" type="file" accept=".csv,text/csv" style="display: none" @change="onFilePicked" />
      </div>
    </header>

    <section class="panel">
      <!-- 两个并列的入口，不是筛选项上多勾一个框。
           删掉的行留着是为了事后查账，混在日常列表里只会让每天要清队列的人
           多筛一道；而查账的人要的是「只看删掉的那些」。 -->
      <el-radio-group v-model="view" class="view-tabs" @change="reload">
        <el-radio-button value="live">{{ t('bankTransactions.viewLive') }}</el-radio-button>
        <el-radio-button value="deleted">{{ t('bankTransactions.viewDeleted') }}</el-radio-button>
      </el-radio-group>

      <div class="filters">
        <el-select v-model="ownership" clearable :placeholder="t('bankTransactions.ownershipAll')" style="width: 150px" @change="reload">
          <el-option value="PENDING" :label="t('bankTransactions.ownerships.PENDING')" />
          <el-option v-for="k in OWNERSHIPS" :key="k" :value="k" :label="t(`bankTransactions.ownerships.${k}`)" />
        </el-select>
        <el-select v-model="direction" clearable :placeholder="t('bankTransactions.directionAll')" style="width: 130px" @change="reload">
          <el-option value="DEBIT" :label="t('bankTransactions.debit')" />
          <el-option value="CREDIT" :label="t('bankTransactions.credit')" />
        </el-select>
        <el-input v-model="keyword" clearable :placeholder="t('bankTransactions.search')" style="max-width: 240px" @keyup.enter="reload" />
        <el-button type="primary" @click="reload">{{ t('common.query') }}</el-button>
      </div>

      <el-dialog v-model="deleteOpen" :title="t('bankTransactions.deleteTitle')" width="440px">
        <p class="del-hint">{{ t('bankTransactions.deleteHint') }}</p>
        <el-input
          v-model="deleteReason"
          type="textarea"
          :rows="3"
          maxlength="500"
          show-word-limit
          :placeholder="t('bankTransactions.deleteReasonPlaceholder')"
        />
        <template #footer>
          <el-button @click="deleteOpen = false">{{ t('common.cancel') }}</el-button>
          <el-button type="danger" :loading="deleting" @click="confirmDelete">
            {{ t('common.delete') }}
          </el-button>
        </template>
      </el-dialog>

      <el-table v-loading="loading" :data="rows" stripe>
        <!-- 只在「已删除」里出现：谁删的、什么时候、为什么。这三样就是
             「作为存档」的全部意思——行还在，理由还在，署名还在。 -->
        <el-table-column
          v-if="view === 'deleted'"
          :label="t('bankTransactions.deletedInfo')"
          width="240"
        >
          <template #default="{ row }">
            <div class="num-cell nowrap">{{ row.deletedAt }}</div>
            <div class="sub">{{ row.deletedBy }}</div>
            <div class="sub del-reason">{{ row.deleteReason }}</div>
          </template>
        </el-table-column>
        <!-- 日期和流水号合成一格：两者回答的是同一个问题——「这一行是哪天、
             哪一笔」。流水号还是搜索和去重的钥匙，所以留全、不截断，只是放小
             一号压在日期底下。合并之前七列在 1280 的笔记本上装不下，右边那个
             固定列会压住「对账单」。 -->
        <el-table-column :label="t('bankTransactions.dateAndRef')" width="176">
          <template #default="{ row }">
            <div class="num-cell nowrap">{{ row.txnDate }}</div>
            <div class="sub num-cell nowrap">{{ row.bankRef }}</div>
          </template>
        </el-table-column>
        <!-- 方向和金额合成一格。原来九列一共要 1500 多像素，容器只有一千出头，
             于是右边那个固定的「操作」列压在「对账单」「匹配状态」上面，中间
             留一道白缝——那不是样式没调好，是列装不下。
             合并也更贴事实：出账还是入账，说的就是这个数往哪边走。 -->
        <el-table-column :label="t('bankTransactions.amount')" width="168" align="right">
          <template #default="{ row }">
            <span class="money-cell" :class="row.direction === 'DEBIT' ? 'is-debit' : 'is-credit'">
              {{ row.direction === 'DEBIT' ? '−' : '+' }}{{ row.currency }} {{ row.amount }}
            </span>
            <div class="sub">
              {{ row.direction === 'DEBIT' ? t('bankTransactions.debit') : t('bankTransactions.credit') }}
            </div>
          </template>
        </el-table-column>
        <!-- 对方和摘要合成一格：一行里它们回答的是同一个问题——「这笔钱是
             跟谁、为着什么事」。摘要里通常是单号，紧挨着户名比隔两列好认。 -->
        <el-table-column :label="t('bankTransactions.counterpartyAndRemark')" min-width="180" show-overflow-tooltip>
          <template #default="{ row }">
            <div class="cp-name">{{ row.counterparty || '—' }}</div>
            <div class="sub">{{ row.remark || '—' }}</div>
          </template>
        </el-table-column>
        <!-- 归属：这笔钱是谁那条线上的。它决定接下来能被谁核销——和方向容易
             被当成一回事，其实不是（供应商退款是进账）。 -->
        <el-table-column :label="t('bankTransactions.ownership')" width="118">
          <template #default="{ row }">
            <el-tag v-if="row.ownership" size="small" effect="plain" :type="ownershipTone(row.ownership)">
              {{ t(`bankTransactions.ownerships.${row.ownership}`) }}
            </el-tag>
            <el-tag v-else size="small" effect="plain" type="info">{{ t('bankTransactions.ownerships.PENDING') }}</el-tag>
            <div v-if="row.ownershipDetail" class="sub">
              {{ t(`bankTransactions.ownershipDetails.${row.ownershipDetail}`) }}
            </div>
          </template>
        </el-table-column>
        <!-- 银行给的那份对账单。摆在这一列而不是藏进详情：财务一眼要看出
             哪几行还没把纸传上来。 -->
        <el-table-column :label="t('bankTransactions.attachment')" width="122">
          <template #default="{ row }">
            <div class="attach-cell">
              <a v-if="row.attachmentUrl" :href="row.attachmentUrl" target="_blank" rel="noopener" class="attach-link">
                {{ row.attachmentName || t('bankTransactions.attachment') }}
              </a>
              <span v-else class="none">{{ t('bankTransactions.noAttachment') }}</span>
              <el-button v-if="canWrite" size="small" link type="primary" :loading="uploadingId === row.id" @click="pickFile(row)">
                {{ row.attachmentKey ? t('bankTransactions.replaceAttachment') : t('bankTransactions.uploadAttachment') }}
              </el-button>
            </div>
          </template>
        </el-table-column>
        <!-- 「匹配付款单」已下线：付款单的创建入口随供应商付款页一起没了，
             而且它和新模型本来就冲突——流水只是记录，不参与核销。存量已经
             对上的付款单号仍然显示，那是历史留痕，读得出来才对得上账。 -->
        <el-table-column :label="t('bankTransactions.matchAndActions')" width="190" fixed="right">
          <template #default="{ row }">
            <div class="match-cell">
              <el-tag v-if="row.matchedPaymentNo" size="small" type="success" effect="plain">{{ row.matchedPaymentNo }}</el-tag>
              <div class="row-actions">
                <!-- noId 而不是 !row.matchedPaymentId：这个字段是 int64，没值的
                     时候到浏览器是字符串 "0"，而 "0" 是真值。写成 ! 的那阵子，
                     「改归属」在任何一行上都不出现。见 lib/protoId.ts。 -->
                <el-button v-if="canWrite && noId(row.matchedPaymentId)" size="small" link @click="openOwnership(row)">
                  {{ t('bankTransactions.setOwnership') }}
                </el-button>
                <!-- 已匹配的历史行留一个「取消匹配」。新建匹配下线了，但改归属
                     那道闸遇到已匹配的行会拒绝并让人「先取消匹配」——不给这个
                     按钮，那些行的归属从此谁也改不了。 -->
                <el-button
                  v-else-if="canWrite" size="small" link type="danger" @click="unmatch(row)"
                >{{ t('bankTransactions.unmatch') }}</el-button>
                <!-- 删是归档：行留着，理由和署名跟着行走。已被认领或已匹配
                     付款单的会被后端拒掉并说清楚先做哪一步。 -->
                <el-button
                  v-if="canWrite && view === 'live'"
                  size="small" link type="danger" @click="openDelete(row)"
                >{{ t('common.delete') }}</el-button>
                <el-button
                  v-if="canWrite && view === 'deleted'"
                  size="small" link type="primary" @click="restore(row)"
                >{{ t('bankTransactions.restore') }}</el-button>
              </div>
            </div>
          </template>
        </el-table-column>
        <template #empty>{{ t('bankTransactions.empty') }}</template>
      </el-table>
      <el-pagination
        class="pager"
        v-model:current-page="page"
        :page-size="20"
        :total="total"
        layout="total, prev, pager, next"
        @current-change="load"
      />
      <input ref="attachInput" type="file" accept="application/pdf,image/*" style="display: none" @change="onAttachPicked" />
    </section>

    <!-- 归属：这笔钱归哪条线。选完之后它才谈得上被谁核销。 -->
    <el-dialog v-model="ownershipOpen" :title="t('bankTransactions.ownershipTitle')" width="min(560px, 94vw)" destroy-on-close>
      <p v-if="ownershipRow" class="pick-context">
        {{ ownershipRow.txnDate }} · {{ ownershipRow.currency }} {{ ownershipRow.amount }} ·
        {{ ownershipRow.counterparty || ownershipRow.bankRef }}
      </p>
      <el-radio-group v-model="ownershipForm.ownership" class="ownership-pick">
        <el-radio value="">{{ t('bankTransactions.ownerships.PENDING') }}</el-radio>
        <el-radio v-for="k in OWNERSHIPS" :key="k" :value="k">{{ t(`bankTransactions.ownerships.${k}`) }}</el-radio>
      </el-radio-group>
      <!-- 二级分类只跟着「不用核销」出现 -->
      <el-select
        v-if="ownershipForm.ownership === 'OTHER'"
        v-model="ownershipForm.detail"
        style="width: 100%; margin-top: 10px"
        :placeholder="t('bankTransactions.ownershipDetailPlaceholder')"
      >
        <el-option v-for="k in OWNERSHIP_DETAILS" :key="k" :value="k" :label="t(`bankTransactions.ownershipDetails.${k}`)" />
      </el-select>
      <el-alert type="info" :closable="false" show-icon style="margin-top: 12px"
        :title="t('bankTransactions.ownershipHint')" />
      <template #footer>
        <el-button @click="ownershipOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="ownershipSaving" @click="saveOwnership">{{ t('common.save') }}</el-button>
      </template>
    </el-dialog>

    <!-- 我们自己的账户：一张清单加一个新增表单。收款对账页撤掉之后搬来
         这里——账户回答的是「钱进/出我们哪个户头」，是这本账的一部分。 -->
    <el-dialog v-model="accountsOpen" :title="t('bankTransactions.accounts')" width="min(640px, 94vw)" destroy-on-close>
      <el-alert type="info" :closable="false" show-icon>{{ t('bankTransactions.accountsHint') }}</el-alert>
      <el-table :data="accounts" size="small" style="margin-top: 10px">
        <el-table-column :label="t('bankTransactions.accountName')" prop="accountName" min-width="170" />
        <el-table-column :label="t('bankTransactions.accountNo')" prop="accountNo" min-width="150" />
        <el-table-column :label="t('bankTransactions.bankName')" prop="bankName" min-width="130" />
        <el-table-column :label="t('bankTransactions.currency')" prop="currency" width="80" />
        <template #empty>{{ t('bankTransactions.accountsEmpty') }}</template>
      </el-table>
      <el-form v-if="canWriteAccounts" :model="accountForm" label-width="90px" style="margin-top: 14px">
        <el-form-item :label="t('bankTransactions.accountName')" required>
          <el-input v-model="accountForm.accountName" />
        </el-form-item>
        <el-form-item :label="t('bankTransactions.accountNo')" required>
          <el-input v-model="accountForm.accountNo" />
        </el-form-item>
        <el-form-item :label="t('bankTransactions.bankName')">
          <el-input v-model="accountForm.bankName" style="width: 240px" />
          <el-select v-model="accountForm.currency" style="width: 110px; margin-left: 12px">
            <el-option v-for="c in CURRENCIES" :key="c" :value="c" :label="c" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="accountsOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button v-if="canWriteAccounts" type="primary" :loading="savingAccount" @click="submitAccount">
          {{ t('bankTransactions.addAccount') }}
        </el-button>
      </template>
    </el-dialog>

    <!-- 手工登记。CSV 之外的另一条入口：银行还没出对账单、或者对方先发了
         水单，先把这笔钱记下来。和导入落同一张表、同一套流水号去重——
         之后再导对账单，同号的行自动跳过，不会记重。 -->
    <el-dialog
      v-model="recordOpen"
      :title="t('bankTransactions.recordTitle')"
      width="min(680px, 94vw)"
      class="record-dialog"
      destroy-on-close
    >
      <!-- 标签放在字段上方：十个字段挤在左侧对齐的标签栏后面，读起来是一堵
           墙；标签在上、字段成两列，一眼能看出「这一段在问什么」。 -->
      <!-- 「这个对话框是干嘛的」放在最上面，而不是压在页脚。它解释的是来这里
           的理由，读者需要它的时刻是动手之前，不是按保存之前。 -->
      <p class="dialog-intro">{{ t('bankTransactions.recordHint') }}</p>
      <el-form label-position="top" class="record-form">
        <section class="fieldset">
          <h4 class="fieldset-title">{{ t('bankTransactions.sectionMoney') }}</h4>
          <div class="grid">
            <el-form-item class="span-2" :label="t('bankTransactions.direction')">
              <!-- 进账绿、出账橙，和列表里那一列同一套颜色。方向选错是最贵的
                   错误之一，让它在选中的那一刻就有颜色。 -->
              <el-radio-group
                v-model="recordForm.direction"
                :class="['direction-pick', recordForm.direction === 'CREDIT' ? 'is-credit' : 'is-debit']"
              >
                <el-radio-button value="CREDIT">{{ t('bankTransactions.credit') }}</el-radio-button>
                <el-radio-button value="DEBIT">{{ t('bankTransactions.debit') }}</el-radio-button>
              </el-radio-group>
            </el-form-item>
            <el-form-item :label="t('bankTransactions.amount')" required>
              <el-input v-model="recordForm.amount" class="amount-input" placeholder="0.00">
                <template #append>
                  <el-select v-model="recordForm.currency" class="currency-select">
                    <el-option v-for="c in CURRENCIES" :key="c" :value="c" :label="c" />
                  </el-select>
                </template>
              </el-input>
            </el-form-item>
            <el-form-item :label="t('bankTransactions.date')" required>
              <el-date-picker
                v-model="recordForm.txnDate" type="date" value-format="YYYY-MM-DD"
                style="width: 100%" :placeholder="t('bankTransactions.datePlaceholder')"
              />
            </el-form-item>
          </div>
        </section>

        <section class="fieldset">
          <h4 class="fieldset-title">{{ t('bankTransactions.sectionWhere') }}</h4>
          <!-- 钱进/出我们哪个户头。选填——CSV 导进来的行本来也没有这个信息。
               能选也能写：清单里没有的账户，当场写一个就建出来，不用先跳去
               「收款账户」再回来重填一遍这张表。 -->
          <div class="grid">
          <el-form-item v-if="canReadAccounts" class="span-2" :label="t('bankTransactions.account')">
            <el-select
              v-model="recordForm.accountId"
              filterable
              clearable
              :allow-create="canWriteAccounts"
              :reserve-keyword="false"
              class="account-select"
              :placeholder="t('bankTransactions.accountPlaceholder')"
            >
              <!-- 收起来时只显示户名和币种；展开的每一行才把账号和开户行摊开。
                   label 里塞着全部信息是为了让输入能搜到账号——el-select 按
                   label 过滤。 -->
              <template #label="{ label }">{{ shortAccountLabel(label) }}</template>
              <el-option
                v-for="a in accounts"
                :key="a.id"
                :value="String(a.id)"
                :label="`${a.accountName} · ${a.accountNo} · ${a.bankName} · ${a.currency}`"
              >
                <div class="acct-row">
                  <span class="acct-name">{{ a.accountName }}</span>
                  <span class="acct-meta">{{ a.accountNo }}<template v-if="a.bankName"> · {{ a.bankName }}</template></span>
                  <el-tag size="small" effect="plain" class="acct-cur">{{ a.currency }}</el-tag>
                </div>
              </el-option>
            </el-select>
            <div class="field-note">
              <span v-if="accountMismatch" class="warn-note">
                {{ t('bankTransactions.accountCurrencyMismatch', { acct: accountMismatch, txn: recordForm.currency }) }}
              </span>
              <span v-else-if="isNewAccount" class="warn-note">
                {{ t('bankTransactions.accountWillBeCreated', { name: recordForm.accountId }) }}
              </span>
              <span v-else-if="canWriteAccounts" class="sub">{{ t('bankTransactions.accountHint') }}</span>
            </div>
          </el-form-item>
            <el-form-item :label="t('bankTransactions.bankRef')" required>
              <el-input v-model="recordForm.bankRef" :placeholder="t('bankTransactions.bankRefPlaceholder')" />
            </el-form-item>
            <el-form-item :label="t('bankTransactions.counterparty')">
              <el-input v-model="recordForm.counterparty" />
            </el-form-item>
            <el-form-item class="span-2" :label="t('bankTransactions.remittance')">
              <el-input v-model="recordForm.remittanceInfo" :placeholder="t('bankTransactions.remittanceHint')" />
            </el-form-item>
          </div>
        </section>

        <section class="fieldset">
          <h4 class="fieldset-title">{{ t('bankTransactions.sectionTag') }}</h4>
          <div class="grid">
            <!-- 登记的人往往当场就知道这是谁那条线上的钱，让他直接写上；
                 不知道就留「待处理」，两条线的队列都看得见它。 -->
            <el-form-item :class="{ 'span-2': recordForm.ownership !== 'OTHER' }" :label="t('bankTransactions.ownership')">
              <el-select v-model="recordForm.ownership" style="width: 100%">
                <el-option value="" :label="t('bankTransactions.ownerships.PENDING')" />
                <el-option v-for="k in OWNERSHIPS" :key="k" :value="k" :label="t(`bankTransactions.ownerships.${k}`)" />
              </el-select>
            </el-form-item>
            <el-form-item v-if="recordForm.ownership === 'OTHER'" :label="t('bankTransactions.ownershipDetailLabel')">
              <el-select v-model="recordForm.detail" style="width: 100%" :placeholder="t('bankTransactions.ownershipDetailPlaceholder')">
                <el-option v-for="k in OWNERSHIP_DETAILS" :key="k" :value="k" :label="t(`bankTransactions.ownershipDetails.${k}`)" />
              </el-select>
            </el-form-item>
            <!-- 对账单跟着这笔一起交。登记这笔钱的人手里正拿着那张纸，让他
                 当场传完，比事后回列表里找哪几行还缺凭证省一趟。 -->
            <el-form-item class="span-2" :label="t('bankTransactions.attachment')">
            <div v-if="recordFile" class="file-chip">
              <span class="file-name">{{ recordFile.name }}</span>
              <el-button link type="primary" @click="recordFileInput?.click()">
                {{ t('bankTransactions.replaceAttachment') }}
              </el-button>
              <el-button link type="danger" @click="clearRecordFile">{{ t('common.remove') }}</el-button>
            </div>
            <button v-else type="button" class="file-drop" @click="recordFileInput?.click()">
              <span class="file-drop-main">{{ t('bankTransactions.pickAttachment') }}</span>
              <span class="sub">{{ t('bankTransactions.pickAttachmentHint') }}</span>
            </button>
            </el-form-item>
          </div>
        </section>
      </el-form>
      <input ref="recordFileInput" type="file" accept="application/pdf,image/*" style="display: none" @change="onRecordFilePicked" />
      <template #footer>
        <el-button @click="recordOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="recording" @click="saveRecord">{{ t('common.save') }}</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="errorsOpen" :title="t('bankTransactions.importErrors')" width="min(560px, 94vw)">
      <el-table :data="importErrors" size="small">
        <el-table-column prop="rowNo" :label="t('bankTransactions.rowNo')" width="90" />
        <el-table-column prop="reason" :label="t('bankTransactions.reason')" min-width="200" />
      </el-table>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { get, post } from '../api'
import { uploadBankStatement } from '../lib/statementUpload'
import { CURRENCIES } from '../constants'
import { noId } from '../lib/protoId'
import { useAuthStore } from '../stores/auth'

const { t } = useI18n()
const auth = useAuthStore()
const canWrite = computed(() => auth.can('procurement:payment:write'))
const canReadAccounts = computed(() => auth.can('export:receipt:read'))
// 建账户走的是 export:receipt:write，**不是本页那个 canWrite**
// （procurement:payment:write）。这两个权限是两回事：搬过来之前账户表单
// 住在收款对账页上，那页的 canWrite 恰好就是 export:receipt:write，
// 口径是对齐的；照搬本页的 canWrite 会两个方向都错——有采购写权限没
// 收款写权限的人看得见按钮、点下去 403，反过来有收款写权限的人明明
// 建得了却看不见表单。
const canWriteAccounts = computed(() => auth.can('export:receipt:write'))

interface TxnRow {
  id: string
  txnDate: string
  direction: string
  amount: string
  currency: string
  counterparty: string
  bankRef: string
  remark: string
  matchedPaymentId: string
  matchedPaymentNo: string
  suggestedPaymentId: string
  suggestedPaymentNo: string
  // 这笔钱是谁那条线上的。空串 = 待处理。
  ownership: string
  // 只有 ownership='OTHER' 时才有值。
  ownershipDetail: string
  suggestedPaymentSupplier: string
  // 银行给的那份对账单。url 是每次读的时候现签的，不要缓存。
  attachmentKey: string
  attachmentUrl: string
  attachmentName: string
  // 删除留痕。**只在「已删除」那个视图里有值**——活着的行这三个是空的。
  deletedAt: string
  deletedBy: string
  deleteReason: string
}
interface PaymentOpt { id: string; paymentNo: string; supplierName: string; currency: string; amount: string; paidAt: string; bankRef: string }
interface RowError { rowNo: number; reason: string }

const rows = ref<TxnRow[]>([])
const attachInput = ref<HTMLInputElement | null>(null)
const attachingRow = ref<TxnRow | null>(null)
const uploadingId = ref('')
const loading = ref(false)
const page = ref(1)
const total = ref(0)
const direction = ref('')
// 归属筛选。'PENDING' 是界面上的第五档「待处理」，发给后端时变成
// ownership_pending=1——后端那边空串已经是「不筛」的意思，一个值不能同时
// 表示「全都要」和「只要没归的」。
const ownership = ref('')
const OWNERSHIPS = ['CUSTOMER', 'SUPPLIER', 'TAX_REFUND', 'OTHER'] as const
const keyword = ref('')

// 「不用核销」下面的二级分类。只有这一档有二级分类——别的档填了也没意义，
// 后端会拒绝。
const OWNERSHIP_DETAILS = ['INTEREST', 'INTERNAL', 'DEPOSIT_RETURN', 'OTHER'] as const

function ownershipTone(v: string): 'success' | 'warning' | 'info' | 'primary' {
  if (v === 'CUSTOMER') return 'success'
  if (v === 'SUPPLIER') return 'warning'
  if (v === 'TAX_REFUND') return 'primary'
  return 'info'
}

const recordOpen = ref(false)
const recording = ref(false)
const recordForm = reactive({
  direction: 'CREDIT', amount: '', currency: 'USD', txnDate: '',
  bankRef: '', counterparty: '', remittanceInfo: '', ownership: '', detail: '',
  accountId: '',
})

// ── 我们自己的账户 ────────────────────────────────────────
//
// 原来住在收款对账页上；那页撤掉之后搬来这里，账户本来就是这本账的一部分。
// 接口挂在 export:receipt:* 上（数据经出口服务代理），而本页的门是
// procurement:payment:*——采购经理有后者、没有前者，所以入口按前者显示，
// 否则他点开就是一个没头没脑的 403。
interface Account { id: string; accountNo: string; accountName: string; bankName: string; currency: string }
const accounts = ref<Account[]>([])
const accountsOpen = ref(false)
const savingAccount = ref(false)
const accountForm = reactive({ accountNo: '', accountName: '', bankName: '', currency: 'USD' })

async function loadAccounts() {
  if (!canReadAccounts.value) return
  accounts.value = (await get<{ accounts: Account[] }>('/bank-accounts')).accounts ?? []
}

async function openAccounts() {
  accountsOpen.value = true
  await loadAccounts()
}

async function submitAccount() {
  if (!accountForm.accountName.trim() || !accountForm.accountNo.trim()) {
    ElMessage.warning(t('bankTransactions.accountIncomplete'))
    return
  }
  savingAccount.value = true
  try {
    await post('/bank-accounts', {
      account_no: accountForm.accountNo, account_name: accountForm.accountName,
      bank_name: accountForm.bankName, currency: accountForm.currency,
    })
    Object.assign(accountForm, { accountNo: '', accountName: '', bankName: '', currency: 'USD' })
    await loadAccounts()
    ElMessage.success(t('bankTransactions.accountAdded'))
  } catch { /* surfaced by the api layer */ } finally {
    savingAccount.value = false
  }
}

// 登记时随手带上的那张对账单。存文件本身而不是先传上去：这一刻还没有
// 流水行，直传地址是按行的 id 签的（key 前缀里带着它，那也是唯一的跨租户
// 隔离），所以只能等行落库之后再传。
const recordFileInput = ref<HTMLInputElement | null>(null)
const recordFile = ref<File | null>(null)

function onRecordFilePicked(e: Event) {
  const input = e.target as HTMLInputElement
  recordFile.value = input.files?.[0] ?? null
  // 清空 input：同一个文件连选两次，不清的话 change 不会再触发。
  input.value = ''
}

function clearRecordFile() {
  recordFile.value = null
}

// 收起来时账户只显示户名和币种。option 的 label 里塞着账号和开户行是为了
// 让输入能搜到它们——el-select 按 label 过滤——但那一整串放在收起的框里
// 会被截断成一段看不懂的字。
function shortAccountLabel(label: string): string {
  const parts = String(label).split(' · ')
  if (parts.length < 2) return String(label)
  return `${parts[0]} · ${parts[parts.length - 1]}`
}

// 选中的是清单里的哪一个。allow-create 之后 accountId 可能是一段用户手写的
// 文字，那时这里是 undefined。
const pickedAccount = computed(() =>
  accounts.value.find((a) => String(a.id) === recordForm.accountId))

// 手写了一个清单里没有的。空串（清空）不算。
const isNewAccount = computed(() =>
  !!recordForm.accountId && !pickedAccount.value)

// 币种对不上只提示、不拦。把 USD 的钱记进人民币户是个真错误，但拦住它就等于
// 说「系统比你清楚」——而临时借个户头收款这种事也真的会发生。
const accountMismatch = computed(() => {
  const a = pickedAccount.value
  return a && a.currency !== recordForm.currency ? `${a.accountName}（${a.currency}）` : ''
})

// 落库前把账户这一格变成一个 id。
//
// **先于建流水行**：手写的账户建失败（比如账号撞了已有的），这一步就断在
// 这里，一行都没写进去。反过来先建流水再建账户，失败之后流水已经在库里、
// 户头还没有，用户看到的是一个说不清成没成的错误。
async function resolveAccountID(): Promise<string> {
  const raw = recordForm.accountId.trim()
  if (!raw) return '0'
  if (pickedAccount.value) return raw
  // 清单里没有 = 用户当场写的。建一个真账户，而不是把这段文字丢掉：
  // 流水行上只有 account_id 一列，没有地方存一段无主的账户名。
  const created = await post<{ id: string }>('/bank-accounts', {
    account_no: raw, account_name: raw, bank_name: '',
    currency: recordForm.currency,
  })
  await loadAccounts()
  return String(created.id)
}

function openRecord() {
  Object.assign(recordForm, {
    direction: 'CREDIT', amount: '', currency: 'USD', txnDate: '',
    bankRef: '', counterparty: '', remittanceInfo: '', ownership: '', detail: '',
    accountId: '',
  })
  recordFile.value = null
  recordOpen.value = true
  void loadAccounts()
}

async function saveRecord() {
  // 只拦「空着没填」，格式和去重交给服务端——它的报错本来就是人话
  // （「流水号 X 已经登记过了。要么这笔钱记过一次，要么号敲错了。」）。
  if (!recordForm.amount || !recordForm.txnDate || !recordForm.bankRef) {
    ElMessage.warning(t('bankTransactions.recordIncomplete'))
    return
  }
  recording.value = true
  try {
    const accountId = await resolveAccountID()
    const created = await post<{ transaction: { id: string } }>('/bank-transactions', {
      transaction: {
        direction: recordForm.direction, amount: recordForm.amount,
        currency: recordForm.currency, txnDate: recordForm.txnDate,
        accountId,
        bankRef: recordForm.bankRef, counterparty: recordForm.counterparty,
        remittanceInfo: recordForm.remittanceInfo,
        ownership: recordForm.ownership,
        ownershipDetail: recordForm.ownership === 'OTHER' ? recordForm.detail : '',
      },
    })
    // 钱先落地，纸随后。两步分开成败，是因为它们的分量不一样：这笔钱记住了
    // 才是要紧的，凭证没传上去是可以回头补的。所以下面那句 catch 不把整个
    // 登记算失败——只说清楚「行进去了、纸没进去」，让人知道该补哪一步。
    const txnID = created?.transaction?.id
    if (recordFile.value && txnID) {
      try {
        await uploadStatement(txnID, recordFile.value)
        ElMessage.success(t('bankTransactions.recordedWithFile'))
      } catch {
        ElMessage.warning(t('bankTransactions.recordedFileFailed'))
      }
    } else {
      ElMessage.success(t('bankTransactions.recorded'))
    }
    recordOpen.value = false
    recordFile.value = null
    await load()
  } finally {
    recording.value = false
  }
}

const ownershipOpen = ref(false)
const ownershipSaving = ref(false)
const ownershipRow = ref<TxnRow | null>(null)
const ownershipForm = reactive({ ownership: '', detail: '' })

// 删除对话框。理由必填——前端也拦一道，省得人写完一堆字才被后端退回来。
const deleteRow = ref<TxnRow | null>(null)
const deleteOpen = ref(false)
const deleteReason = ref('')
const deleting = ref(false)

function openDelete(row: TxnRow) {
  deleteRow.value = row
  deleteReason.value = ''
  deleteOpen.value = true
}

async function confirmDelete() {
  const row = deleteRow.value
  if (!row) return
  if (!deleteReason.value.trim()) {
    ElMessage.warning(t('bankTransactions.deleteReasonRequired'))
    return
  }
  deleting.value = true
  try {
    await post(`/bank-transactions/${row.id}/delete`, { reason: deleteReason.value.trim() })
    ElMessage.success(t('bankTransactions.deleted'))
    deleteOpen.value = false
    await load()
  } finally {
    deleting.value = false
  }
}

// 从已删除放回列表。误删之后重新登记同一笔会被流水号的唯一键挡住，而那一行
// 在列表里又看不见——没有这条路，人只会觉得系统在胡说。
async function restore(row: TxnRow) {
  await ElMessageBox.confirm(
    t('bankTransactions.restoreConfirm', { ref: row.bankRef }),
    t('bankTransactions.restore'),
  )
  await post(`/bank-transactions/${row.id}/restore`, {})
  ElMessage.success(t('bankTransactions.restored'))
  await load()
}

function openOwnership(row: TxnRow) {
  ownershipRow.value = row
  ownershipForm.ownership = row.ownership || ''
  ownershipForm.detail = row.ownershipDetail || ''
  ownershipOpen.value = true
}

async function saveOwnership() {
  const row = ownershipRow.value
  if (!row) return
  ownershipSaving.value = true
  try {
    await post(`/bank-transactions/${row.id}/ownership`, {
      ownership: ownershipForm.ownership,
      // 二级分类只跟着「不用核销」走。别的档带着它，后端会拒绝——这里先
      // 清掉，免得用户切换归属之后被一个看不见的旧值挡住。
      ownership_detail: ownershipForm.ownership === 'OTHER' ? ownershipForm.detail : '',
    })
    ownershipOpen.value = false
    ElMessage.success(t('bankTransactions.ownershipSaved'))
    await load()
  } finally {
    ownershipSaving.value = false
  }
}
const defaultCurrency = ref('')
const importing = ref(false)
// 看活着的还是看已删除的。两者互斥——见模板上那段注释。
const view = ref<'live' | 'deleted'>('live')
const fileInput = ref<HTMLInputElement | null>(null)
const importErrors = ref<RowError[]>([])
const errorsOpen = ref(false)

async function load() {
  loading.value = true
  try {
    const resp = await get<{ items: TxnRow[]; total: string }>('/bank-transactions', {
      page: page.value, page_size: 20, direction: direction.value, keyword: keyword.value,
      ownership: ownership.value === 'PENDING' ? '' : ownership.value,
      ownership_pending: ownership.value === 'PENDING' ? '1' : '',
      deleted: view.value === 'deleted' ? '1' : '',
    })
    rows.value = resp.items || []
    total.value = Number(resp.total || 0)
  } finally {
    loading.value = false
  }
}
function reload() { page.value = 1; void load() }

function fileBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result).split(',')[1] || '')
    reader.onerror = reject
    reader.readAsDataURL(file)
  })
}

async function onFilePicked(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  importing.value = true
  try {
    const resp = await post<{ imported: number; duplicates: number; errors?: RowError[] }>(
      '/bank-transactions/import',
      { fileName: file.name, data: await fileBase64(file), defaultCurrency: defaultCurrency.value },
    )
    ElMessage.success(t('bankTransactions.imported', { n: resp.imported || 0, d: resp.duplicates || 0 }))
    if (resp.errors?.length) {
      importErrors.value = resp.errors
      errorsOpen.value = true
    }
    reload()
  } catch {
    /* the api layer already surfaced the server's message */
  } finally {
    importing.value = false
    input.value = ''
  }
}

// 新建匹配已下线（选付款单、采纳建议）。**解开历史匹配留着**：改归属那道
// 闸遇到已匹配的行会拒绝并让人「先取消匹配」，没有这条路那句话就是个做不到
// 的指令，那些行的归属从此谁也改不了。
async function unmatch(row: TxnRow) {
  await post(`/bank-transactions/${row.id}/unmatch`, {})
  ElMessage.success(t('bankTransactions.unmatchedOk'))
  void load()
}

// 对账单那张纸走 lib/statementUpload 的三步直传，两个入口共用（登记对话框里
// 随手带的、和列表里事后补的）。那三步漏一步都不报错、只是纸悄悄没上去，
// 所以放在 lib 里单测，而不是在这里各写一遍。
function uploadStatement(txnID: string, file: File) {
  return uploadBankStatement(txnID, file, { post })
}

function pickFile(row: TxnRow) {
  attachingRow.value = row
  attachInput.value?.click()
}

async function onAttachPicked(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  const row = attachingRow.value
  if (!file || !row) return
  uploadingId.value = row.id
  try {
    await uploadStatement(row.id, file)
    ElMessage.success(t('bankTransactions.attachmentUploaded'))
    void load()
  } catch {
    ElMessage.error(t('bankTransactions.attachmentFailed'))
  } finally {
    uploadingId.value = ''
    attachingRow.value = null
    input.value = ''
  }
}

onMounted(load)
</script>

<style scoped>
.view-tabs {
  margin-bottom: 12px;
}
.del-hint {
  margin: 0 0 10px;
  font-size: 13px;
  line-height: 1.6;
  color: var(--el-text-color-secondary);
}
.del-reason {
  white-space: normal;
  word-break: break-word;
}
.suggest { color: var(--el-color-primary); font-size: 13px; }
.ownership-pick {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 18px;
}
.sub {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.none { color: var(--el-text-color-secondary); }
/* ── 登记流水对话框 ────────────────────────────────────────
   十个字段分成三段，每段一个小标题。字段本身是两列网格，窄的（金额、
   日期、归属）并排，宽的（附言、账户）通栏——比一律左对齐标签 + 单列
   少一半高度，而且「这一段在问什么」看得出来。 */
.record-form :deep(.el-form-item) {
  margin-bottom: 12px;
}
.record-form :deep(.el-form-item__label) {
  padding-bottom: 2px;
  font-size: 13px;
  color: var(--el-text-color-regular);
}
.fieldset + .fieldset {
  margin-top: 4px;
  padding-top: 12px;
  border-top: 1px solid var(--el-border-color-lighter);
}
.fieldset-title {
  margin: 0 0 10px;
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}
/* 每段标题前一道短竖线。三段是三个问题，有个视觉锚点比纯靠间距分得开。 */
.fieldset-title::before {
  content: '';
  display: inline-block;
  width: 3px;
  height: 12px;
  margin-right: 8px;
  vertical-align: -1px;
  border-radius: 2px;
  background: var(--el-color-primary);
  opacity: 0.55;
}
.dialog-intro {
  margin: -4px 0 16px;
  padding: 10px 12px;
  border-radius: 8px;
  background: var(--el-fill-color-light);
  font-size: 12px;
  line-height: 1.6;
  color: var(--el-text-color-secondary);
}
/* **固定两列**，不是 auto-fit。
   原来写的是 repeat(auto-fit, minmax(190px, 1fr))：能塞几列就塞几列，于是
   「这笔钱」那段三个字段排成三列、下面两段排成两列——每段的列边落在不同
   位置，整张表看着就是参差的。列数写死，四条边才对得齐。 */
.grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0 16px;
}
/* 通栏的字段（账户、附言、对账单）横跨两列，它们的左右边和上面那两列的
   最外侧对齐——一整张表只有两条竖直参考线。 */
.grid > .span-2 {
  grid-column: 1 / -1;
}
/* 窄屏塌成一列，省得把日期挤成一条缝。 */
@media (max-width: 560px) {
  .grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
/* 分段控件撑满自己那一格：它原来按内容宽度缩在左边，右边空一大块，
   而隔壁两个字段是满格的——一眼就看出来没对齐。 */
.direction-pick {
  display: flex;
  width: 100%;
  /* 通栏那一行里控件本身收到 320：两段的切换器铺满一整行像条横幅，
     但左边缘仍和其它字段对齐，所以看着是「有意留白」而不是「没对齐」。 */
  max-width: 320px;
}
.direction-pick :deep(.el-radio-button) {
  flex: 1;
}
.direction-pick :deep(.el-radio-button__inner) {
  width: 100%;
}

/* 进账绿、出账橙——和列表里那一列同一套颜色。方向选错是最贵的错误之一，
   让它在选中的那一刻就有颜色，而不是等保存完回列表才发现。 */
.direction-pick.is-credit :deep(.el-radio-button__original-radio:checked + .el-radio-button__inner) {
  background: var(--el-color-success);
  border-color: var(--el-color-success);
  box-shadow: -1px 0 0 0 var(--el-color-success);
}
.direction-pick.is-debit :deep(.el-radio-button__original-radio:checked + .el-radio-button__inner) {
  background: var(--el-color-warning);
  border-color: var(--el-color-warning);
  box-shadow: -1px 0 0 0 var(--el-color-warning);
}

/* 金额是这张表里唯一要一眼读准的数：等宽字形，位数对齐。 */
.amount-input :deep(.el-input__inner) {
  font-variant-numeric: tabular-nums;
  font-size: 15px;
  letter-spacing: 0.3px;
}
/* 金额和币种是**一个**字段的两半，所以拼成一个输入框组，而不是并排两个
   控件——并排的话「0.00」和「USD」之间会有一道 gap，读起来像两件事。
//
   附加段这三条缺一不可：
   · width 写死：组的左右两半按它切，不写就由内容撑，撑多宽看币种字数
   · padding 0 **必须**配 select 的 margin 0。Element 默认给附加段
     padding: 0 20px，又给里面的 select 配了 margin: -10px -20px 去抵消它。
     只清 padding 不清 margin，那对负边距就把下拉顶到框外——实测下拉
     632→724 而附加段只有 652→704，箭头越过整个字段右边界 8px。
   · 背景透明 + 只留左边一道分隔线：默认那块灰底会让币种看着像另一个
     控件，而它是这个金额的一部分。 */
.amount-input :deep(.el-input-group__append) {
  width: 92px;
  padding: 0;
  /* **只去灰底，不去边框。** 那圈边是 Element 用一组 inset 阴影画的
     （上、下、右，外加左边那道分隔线），写 box-shadow: none 会把整段的
     外框一起抹掉——USD 那一半就没有右边框了，字段像是缺了一角。 */
  background: transparent;
}
.amount-input :deep(.el-input-group__append .el-select) {
  width: 100%;
  margin: 0;
}
/* 币种就三个字母，居中比左对齐稳——左对齐时文字和右边的箭头之间会空出
   一大截，看着像没填满。 */
.amount-input :deep(.el-input-group__append .el-select__wrapper) {
  box-shadow: none;
  background: transparent;
  padding-left: 12px;
  padding-right: 8px;
}

.account-select {
  width: 100%;
}
.acct-row {
  display: flex;
  align-items: center;
  gap: 10px;
}
.acct-name {
  font-weight: 500;
}
.acct-meta {
  flex: 1;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  font-variant-numeric: tabular-nums;
}
.acct-cur {
  flex: none;
}
.field-note {
  margin-top: 2px;
  line-height: 1.5;
}
.warn-note {
  font-size: 12px;
  color: var(--el-color-warning);
}

/* 附件那一格：没选时是一块可点的区域而不是一个孤零零的按钮——它要接住
   「我手上这张纸放哪」这个念头，按钮太小声。 */
.file-drop {
  display: flex;
  flex-direction: column;
  gap: 2px;
  width: 100%;
  padding: 12px 14px;
  text-align: left;
  border: 1px dashed var(--el-border-color);
  border-radius: 8px;
  background: var(--el-fill-color-blank);
  cursor: pointer;
  transition: border-color var(--el-transition-duration), background-color var(--el-transition-duration);
}
.file-drop:hover {
  border-color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
}
.file-drop-main {
  font-size: 14px;
  color: var(--el-color-primary);
}
.file-chip {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 8px 12px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  background: var(--el-fill-color-light);
  flex-wrap: wrap;
}
.file-name {
  flex: 1;
  min-width: 0;
  font-size: 13px;
  color: var(--el-text-color-regular);
  word-break: break-all;
}
.attach-link { color: var(--el-color-primary); text-decoration: none; }
.attach-link:hover { text-decoration: underline; }
.pick-context { margin: 0 0 12px; color: var(--el-text-color-secondary); }

/* ── 页面外壳 ──────────────────────────────────────────────
   这一页原来一条布局样式都没有：.page / .page-head / .panel / .filters
   四个类名在模板里写着，却谁也没定义（同区的客户对账、供应商对账各自带
   一份，这一页漏了）。结果就是标题和右上角那排按钮堆成上下两行、表格
   直接贴在页面上、筛选条和表头之间没有一点呼吸。
   这里补的就是那一份，键值照着同区其它页抄，让财务这几页看着是一套东西。 */
.page {
  display: flex;
  flex-direction: column;
  gap: 18px;
}
.page-head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
}
.page-head .eyebrow {
  font-size: 12px;
  letter-spacing: 1.5px;
  color: var(--el-text-color-secondary);
}
.page-head h1 {
  margin: 4px 0 6px;
  font-size: 28px;
}
.page-head p {
  margin: 0;
  color: var(--el-text-color-regular);
}
.panel {
  padding: 16px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 10px;
  background: var(--el-bg-color);
}
.filters {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 14px;
  flex-wrap: wrap;
}
.pager {
  margin-top: 14px;
  justify-content: flex-end;
}
/* 金额、流水号、日期都是要竖着比对的数：等宽字形，位数对齐。 */
.num-cell {
  font-variant-numeric: tabular-nums;
}
.money-cell {
  font-variant-numeric: tabular-nums;
  font-weight: 600;
}
.attach-cell {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 2px;
}
.attach-cell .attach-link {
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.nowrap {
  white-space: nowrap;
}
/* 出账带减号染橙、入账带加号染绿。方向从独立一列并进金额里之后，符号和
   颜色就是方向本身——扫一列数字比扫一列标签快。 */
.money-cell.is-debit {
  color: var(--el-color-warning);
}
.money-cell.is-credit {
  color: var(--el-color-success);
}
.cp-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.match-cell {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 2px;
}
/* 一行里最多三个按钮，横排、间距靠 gap 而不是 el-button 自带的 margin
   ——link 型按钮之间默认没有间距，挤在一起会被读成一个词。 */
.row-actions {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}
.row-actions :deep(.el-button + .el-button) {
  margin-left: 0;
}
.head-actions {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
}
</style>

<!-- el-dialog 会被 teleport 到 body，作用域样式够不着它（scoped 的属性只落在
     本组件模板里的元素上，而 .el-dialog 是子组件的内部结构）。这两条必须
     全局，靠 .record-dialog 这个独一份的类名把范围收住。 -->
<style>
/* 十个字段撑到 800 多像素，在 768 高的笔记本上「保存」会掉到折线以下。
   表单区自己滚、页脚钉住——要按的那个按钮永远在看得见的地方。 */
.record-dialog {
  margin-top: 6vh !important;
}
.record-dialog .el-dialog__body {
  /* 用 calc 而不是 vh 百分比：页眉页脚的高度是固定的几十像素，按比例扣
     在矮屏上扣得太狠、在高屏上又留一大片空。 */
  max-height: calc(100vh - 210px);
  overflow-y: auto;
  padding-right: 16px;
}
</style>
