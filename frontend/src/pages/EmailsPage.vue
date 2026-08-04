<template>
  <MailboxGate v-if="locked === true" @unlocked="onUnlocked" @host-settings="hostOpen = true" />
  <div v-else-if="locked === false" class="mailbox">
    <!-- A folder rail, not tabs. The distinction matters: folders say "your
         mail lives in these places", tabs said "here are three reports". -->
    <aside class="rail">
      <el-button v-if="canWrite" type="primary" class="compose" @click="composing = true">
        {{ t('emails.compose') }}
      </el-button>
      <button
        v-for="f in folders"
        :key="f.key"
        class="folder"
        :class="{ on: folder === f.key }"
        type="button"
        @click="switchFolder(f.key)"
      >
        <el-icon class="ficon"><component :is="f.icon" /></el-icon>
        <span class="fname">{{ t(`emails.folders.${f.key}`) }}</span>
        <el-badge v-if="f.key === 'attention' && attentionCount > 0" :value="attentionCount" />
        <el-badge v-else-if="f.key === 'inbox' && unreadCount > 0" :value="unreadCount" />
        <span v-else-if="f.key === 'drafts' && drafts.length" class="cnt">{{ drafts.length }}</span>
      </button>

      <!-- No colleague picker here. This page is "my mail" and stays that
           way; reading somebody else's is a separate, read-only surface. A
           filter here also let a supervisor requeue or abandon a colleague's
           message, which is the salesperson's call, not theirs. -->

      <span class="rail-grow" />
      <!-- Locks the mailbox, not the ERP: the token dies server-side, so the
           next visitor to this workstation faces the gate again. -->
      <el-button link class="rail-lock" @click="lockMailbox">
        🔒 {{ t('mailGate.signOut') }}
      </el-button>
      <el-button v-if="isAdmin" link class="rail-lock rail-host" @click="hostOpen = true">
        ⚙️ {{ t('mailGate.hostSettings') }}
      </el-button>
    </aside>

    <section class="pane">
      <!-- The mailbox saying it is not receiving. Without this, a revoked
           authorisation fails every poll in silence while the page goes on
           showing the last successful sync as though it were current. -->
      <el-alert
        v-if="syncError"
        type="error"
        :closable="false"
        show-icon
        class="sync-error"
      >
        <div class="sync-error-body">
          <span>{{ t('emails.syncBroken', { e: syncError }) }}</span>
          <el-button size="small" type="primary" plain @click="reauth">
            {{ t('emails.reauth') }}
          </el-button>
        </div>
      </el-alert>

      <!-- ------------------------------------------------- reading a mail -->
      <!-- A page, not a drawer: the mail's id lives in the URL, so a refresh
           reopens the same mail and the browser's back button returns to the
           list, at the page it was on. -->
      <template v-if="openedInbound">
        <div class="detail-top">
          <el-button link class="back-btn" @click="backToList">
            ← {{ t('emails.backToList') }}
          </el-button>
        </div>
        <h2 class="in-subject">{{ openedInbound.subject || t('emails.noSubject') }}</h2>
        <!-- Who wrote it, as a person rather than a field: the avatar gives
             the eye somewhere to land before it starts reading, which is the
             whole reason every mail client has one. -->
        <div class="in-from">
          <span class="avatar" :style="avatarStyle(openedInbound.fromEmail)" aria-hidden="true">
            {{ initialOf(openedInbound.fromName || openedInbound.fromEmail) }}
          </span>
          <div class="in-who">
            <div class="in-meta">
              <span class="strong">{{ openedInbound.fromName || openedInbound.fromEmail }}</span>
              <span class="sub">&lt;{{ openedInbound.fromEmail }}&gt;</span>
            </div>
            <div class="sub">{{ t('emails.inboundTo', { to: openedInbound.toEmail }) }}</div>
          </div>
          <span class="grow" />
          <span class="sub in-when">
            {{ shortTime(openedInbound.sentAt || openedInbound.receivedAt) }}
          </span>
        </div>
        <div class="in-actions">
          <template v-if="canWrite">
            <el-button size="small" type="primary" plain @click="replyToInbound">
              ↩ {{ t('emails.reply') }}
            </el-button>
            <!-- A split button rather than a third one in the row: forwarding
                 as an attachment is the same intent taken further, not a
                 separate errand, and it is rare enough that giving it equal
                 width would misstate how often it is wanted. -->
            <el-dropdown size="small" split-button @click="forwardInbound">
              ↪ {{ t('emails.forward') }}
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item
                    :disabled="!openedInbound.hasRaw"
                    @click="forwardInboundAsAttachment"
                  >
                    {{ t('emails.forwardAsAttachment') }}
                  </el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </template>
          <el-button
            v-if="folder === 'junk'"
            size="small"
            type="warning"
            plain
            @click="markOpened({ notJunk: true })"
          >
            {{ t('emails.notJunk') }}
          </el-button>
          <template v-if="isInboundView && folder !== 'junk'">
            <el-button v-if="folder !== 'trash'" size="small" plain @click="markOpened({ read: false })">
              {{ t('emails.markUnread') }}
            </el-button>
            <el-button v-if="folder === 'archive'" size="small" plain @click="markOpened({ archived: false })">
              {{ t('emails.unarchive') }}
            </el-button>
            <el-button v-else-if="folder !== 'trash'" size="small" plain @click="markOpened({ archived: true })">
              {{ t('emails.archive') }}
            </el-button>
            <el-button v-if="folder === 'trash'" size="small" plain @click="markOpened({ deleted: false })">
              {{ t('emails.restore') }}
            </el-button>
            <el-button v-if="folder === 'trash'" size="small" type="danger" plain @click="purgeOpened">
              {{ t('emails.purge') }}
            </el-button>
            <el-button v-if="folder !== 'trash'" size="small" type="danger" plain @click="markOpened({ deleted: true })">
              {{ t('emails.toTrash') }}
            </el-button>
          </template>
        </div>
        <el-divider />
        <!-- The whole exchange when there is one, the single mail otherwise.
             All HTML here was sanitised server-side; see GetInbound and
             GetMailThread. -->
        <template v-if="threadItems.length > 1">
          <div class="thread-count">{{ t('emails.threadCount', { n: threadItems.length }) }}</div>
          <div
            v-for="it in threadItems"
            :key="it.direction + it.id"
            class="thread-item"
            :class="{ out: it.direction === 'OUT' }"
          >
            <button type="button" class="thread-head" @click="toggleThreadItem(it)">
              <el-tag size="small" :type="it.direction === 'OUT' ? 'info' : 'success'" effect="plain">
                {{ it.direction === 'OUT' ? t('emails.threadOut') : t('emails.threadIn') }}
              </el-tag>
              <span class="strong">{{ it.who || it.counterparty }}</span>
              <span class="sub ellipsis">{{ it.counterparty }}</span>
              <span class="grow" />
              <span class="sub">{{ shortTime(it.at) }}</span>
            </button>
            <div v-show="isThreadOpen(it)" class="thread-body">
              <div v-if="it.bodyFormat === 'HTML'" class="in-html" v-html="it.body" />
              <pre v-else class="in-text">{{ it.body }}</pre>
            </div>
          </div>
        </template>
        <template v-else>
          <div v-if="openedInbound.bodyHtml" class="in-html" v-html="openedInbound.bodyHtml" />
          <pre v-else class="in-text">{{ openedInbound.bodyText }}</pre>
        </template>
        <template v-if="openedInbound.attachments?.length">
          <el-divider />
          <h4 class="side-title">{{ t('emails.attachments') }}</h4>
          <!-- Links, not labels. Until now these were plain tags: the file was
               listed, stored, and impossible to get back out. The href is a
               signed, time-limited URL straight to storage, so the bytes never
               pass through the gateway. -->
          <div class="files">
            <div
              v-for="a in openedInbound.attachments"
              :key="a.id"
              class="file"
              :class="{ dead: !a.downloadUrl }"
              :title="fileHint(a)"
            >
              <el-icon><Paperclip /></el-icon>
              <span class="fname ellipsis">{{ a.fileName }}</span>
              <span class="sub">{{ humanSize(Number(a.fileSize)) }}</span>
              <!-- Look and take are separate acts, so they get separate
                   buttons. A preview only appears for what can honestly be
                   shown; for a .pptx or a .zip the download is the whole
                   interaction. -->
              <el-tooltip
                v-if="a.previewUrl"
                :content="t('emails.previewFile')"
                placement="top"
                :show-after="0"
                :hide-after="0"
              >
                <button type="button" class="fbtn" @click="openPreview(a)">
                  <el-icon><View /></el-icon>
                </button>
              </el-tooltip>
              <el-tooltip
                v-if="a.downloadUrl"
                :content="t('emails.downloadFile', { f: a.fileName })"
                placement="top"
                :show-after="0"
                :hide-after="0"
              >
                <a class="fbtn" :href="a.downloadUrl" :download="a.fileName">
                  <el-icon><Download /></el-icon>
                </a>
              </el-tooltip>
            </div>
          </div>
        </template>
      </template>

      <!-- ------------------------------------------- reading a sent mail -->
      <!-- The same page treatment as an inbound mail, and for the same
           reasons. A sent mail used to open in a drawer over the list: no
           address of its own, so no refresh, no back button, no link to send
           a colleague — and a narrower column to read the same mail in. -->
      <template v-else-if="outboundOpen">
        <div class="detail-top">
          <el-button link class="back-btn" @click="backFromOutbound">
            ← {{ t('emails.backToList') }}
          </el-button>
        </div>
        <!-- MailReader renders nothing without a mail, so without this the
             page would be a lone back button until the fetch lands. -->
        <div v-loading="readerLoading" class="reader-slot">
          <MailReader :mail="openMail" />
        </div>
      </template>

      <template v-else>
      <div class="pane-head">
        <!-- Select-all lives in the toolbar, not in a list header: this list
             has no header row, and the toolbar is where the actions are that
             a selection is for. Indeterminate when only some are ticked,
             which is the state that tells you a click will clear rather than
             extend. -->
        <el-checkbox
          v-if="isInboundView && inbound.length > 0"
          class="pick-all"
          :model-value="allPicked"
          :indeterminate="somePicked"
          :aria-label="t('emails.selectAll')"
          @change="toggleAllPicked"
        />
        <!-- With a selection open, the toolbar is about the selection. Gmail
             does the same, and for the same reason: 搜索 and 立即收信 are not
             what somebody who has just ticked six mails is looking for, and
             leaving them there buries the buttons that are. -->
        <template v-if="pickedRows.length">
          <span class="picked-n">{{ t('emails.pickedN', { n: pickedRows.length }) }}</span>
          <span class="grow" />
          <el-button v-if="folder !== 'trash'" size="small" @click="bulkMark({ read: true })">
            {{ t('emails.markRead') }}
          </el-button>
          <el-button v-if="folder !== 'trash'" size="small" @click="bulkMark({ read: false })">
            {{ t('emails.markUnread') }}
          </el-button>
          <el-button v-if="folder === 'junk'" size="small" @click="bulkMark({ notJunk: true })">
            {{ t('emails.notJunk') }}
          </el-button>
          <el-button
            v-if="folder === 'inbox' || folder === 'starred'"
            size="small"
            @click="bulkMark({ archived: true })"
          >
            {{ t('emails.archive') }}
          </el-button>
          <el-button v-if="folder === 'archive'" size="small" @click="bulkMark({ archived: false })">
            {{ t('emails.unarchive') }}
          </el-button>
          <el-button v-if="folder === 'trash'" size="small" @click="bulkMark({ deleted: false })">
            {{ t('emails.restore') }}
          </el-button>
          <el-button
            v-if="folder === 'trash'"
            size="small"
            type="danger"
            plain
            :loading="bulkBusy"
            @click="bulkPurge"
          >
            {{ t('emails.purge') }}
          </el-button>
          <el-button
            v-else
            size="small"
            type="danger"
            plain
            :loading="bulkBusy"
            @click="bulkMark({ deleted: true })"
          >
            {{ t('emails.toTrash') }}
          </el-button>
          <el-button size="small" link @click="picked = []">
            {{ t('emails.clearSelection') }}
          </el-button>
        </template>
        <template v-else>
        <h2>{{ t(`emails.folders.${folder}`) }}</h2>
        <span class="grow" />
        <!-- Not every folder is searchable. The scheduled list is short by
             nature and the query behind it takes no keyword; a box that
             silently ignores what is typed into it is worse than none. -->
        <template v-if="folder !== 'scheduled'">
          <el-input
            v-model="keyword"
            :placeholder="t(`emails.search.${searchKey}`)"
            clearable
            style="width: 260px"
            @keyup.enter="reload"
            @clear="reload"
          />
          <el-button @click="reload">{{ common('query') }}</el-button>
        </template>
        <!-- The mailbox is polled every couple of minutes; this is for the
             person who just told a customer "resend it" and is waiting. -->
        <el-button v-if="folder === 'inbox'" :loading="syncing" @click="syncNow">
          {{ t('emails.syncNow') }}
        </el-button>
        <!-- Clears the unread marks of this view only — the button sits above
             this list, so it does what this list shows.
             Not in junk or the trash: nobody reads their spam folder to the
             end, and what those two need is a way to be rid of it. -->
        <el-button v-if="canMarkAllRead" :loading="markingAll" @click="markAllRead">
          {{ t('emails.markAllRead') }}
        </el-button>
        <!-- Junk out in one click — into the trash, not oblivion. The mail
             worth finding in a spam folder is the customer enquiry the filter
             got wrong, and that is noticed the next day. -->
        <el-button
          v-if="folder === 'junk' && total > 0"
          type="danger"
          plain
          :loading="emptying"
          @click="emptyJunk"
        >
          {{ t('emails.emptyJunk') }}
        </el-button>
        <el-button
          v-if="folder === 'trash' && total > 0"
          type="danger"
          plain
          :loading="emptying"
          @click="emptyTrash"
        >
          {{ t('emails.emptyTrash') }}
        </el-button>
        <el-button v-if="folder === 'suppressions' && canSuppress" @click="openSuppress">
          {{ t('emails.addSuppression') }}
        </el-button>
        </template>
      </div>

      <!-- ------------------------ inbox / starred / archive / junk / trash -->
      <template v-if="isInboundView">
        <!-- Spam is the host's verdict, shown read-only as a safety net: the
             mis-flagged customer inquiry is the one mail worth finding here. -->
        <el-alert
          v-if="folder === 'trash'"
          type="info"
          :closable="false"
          show-icon
          class="junk-note"
        >
          {{ t('emails.trashRetention') }}
        </el-alert>
        <el-alert
          v-if="folder === 'junk'"
          type="info"
          :closable="false"
          show-icon
          class="junk-note"
        >
          {{ t('emails.junkNote') }} {{ t('emails.junkNoteRescue') }}
        </el-alert>
        <MailList
          v-model:selected="picked"
          :mails="inbound"
          :folder="folder"
          :loading="loading"
          @open="openInbound"
          @star="toggleStar"
          @mark="markRow"
          @purge="purgeRow"
        />
        <el-empty
          v-if="!loading && inbound.length === 0"
          :description="t(folder === 'inbox' ? 'emails.emptyInbox' : 'emails.emptyFolder')"
        />
      </template>

      <!-- --------------------------------------------------------- drafts -->
      <template v-else-if="folder === 'drafts'">
      <el-table
        :data="drafts"
        v-loading="loading"
        class="clickable"
        @row-click="openDraft"
      >
        <el-table-column :label="t('emails.subject')" min-width="300">
          <template #default="{ row }">
            <div class="strong ellipsis">{{ row.subject || t('emails.noSubject') }}</div>
            <div class="sub">{{ t('emails.draftRecipients', { n: row.recipientCount }) }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('emails.savedAt')" width="160">
          <template #default="{ row }">
            <span class="sub">{{ shortTime(row.updatedAt) }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="common('actions')" width="90">
          <template #default="{ row }">
            <el-button link type="danger" @click.stop="dropDraft(row)">
              {{ common('delete') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && drafts.length === 0" :description="t('emails.noDrafts')" />
      </template>

      <!-- ------------------------------------------------------ scheduled -->
      <template v-else-if="folder === 'scheduled'">
      <el-alert type="info" :closable="false" show-icon class="hint">
        {{ t('emails.scheduledHint') }}
      </el-alert>
      <el-table :data="scheduled" v-loading="loading">
        <el-table-column :label="t('emails.subject')" min-width="280">
          <template #default="{ row }">
            <div class="strong ellipsis">{{ row.subject || t('emails.noSubject') }}</div>
            <div class="sub ellipsis">{{ row.toNames }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('emails.scheduledRecipients')" width="100">
          <template #default="{ row }">
            <span class="sub">{{ row.pendingCount }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('emails.scheduledWhen')" width="220">
          <template #default="{ row }">
            <div>{{ localTime(row.scheduledAt) }}</div>
            <div class="sub">{{ untilText(row.scheduledAt) }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="common('actions')" width="190">
          <template #default="{ row }">
            <el-button link type="primary" @click.stop="sendScheduledNow(row)">
              {{ t('emails.sendNow') }}
            </el-button>
            <el-button link type="danger" @click.stop="cancelScheduled(row)">
              {{ t('emails.cancelSchedule') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-empty
        v-if="!loading && scheduled.length === 0"
        :description="t('emails.scheduledEmpty')"
      />
      </template>

      <!-- ----------------------------------------------------------- sent -->
      <!-- The same list component as every other folder, because it is the
           same thing: messages. Since the host's copy of an ERP send is kept
           rather than discarded, a sent mail has a folder and a UID like any
           other, so star, archive and delete work here without a special
           case. The exception is a send the host kept no copy of — that is a
           delivery record, and MailList gives it no star and no actions
           rather than buttons that would fail. -->
      <template v-else-if="folder === 'sent'">
        <MailList
          v-model:selected="picked"
          :mails="mailboxSent"
          folder="sent"
          :loading="loading"
          @open="openSentRow"
          @star="toggleStar"
          @mark="markRow"
        />
        <el-empty v-if="!loading && mailboxSent.length === 0" :description="t('emails.emptyFolder')" />
      </template>

      <!-- ------------------------------------------------------ attention -->
      <el-table
        v-else-if="folder === 'attention'"
        :data="messages"
        v-loading="loading"
        class="clickable"
        @row-click="openMessage"
      >
        <el-table-column :label="t('emails.recipient')" min-width="190">
          <template #default="{ row }">
            <div class="strong">{{ row.toName || '—' }}</div>
            <div class="sub">{{ row.toEmail }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="common('status')" width="140">
          <template #default="{ row }">
            <el-tag size="small" :type="statusType(row.status)" effect="plain">
              {{ t(`emails.statuses.${row.status}`) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('emails.whyStuck')" min-width="270">
          <template #default="{ row }">
            <div>{{ row.attentionReason || '—' }}</div>
            <div v-if="row.lastError" class="sub ellipsis" :title="row.lastError">
              {{ row.lastError }}
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="common('actions')" width="150">
          <template #default="{ row }">
            <template v-if="canWrite">
              <el-button link type="primary" @click.stop="openRequeue(row)">
                {{ t('emails.requeue') }}
              </el-button>
              <el-button link type="danger" @click.stop="doAbandon(row)">
                {{ t('emails.abandon') }}
              </el-button>
            </template>
          </template>
        </el-table-column>
      </el-table>

      <!-- --------------------------------------------------- suppressions -->
      <el-table v-else :data="suppressions" v-loading="loading">
        <el-table-column prop="email" :label="t('emails.email')" min-width="230" />
        <el-table-column :label="t('emails.suppressReason')" width="150">
          <template #default="{ row }">
            <el-tag size="small" effect="plain" :type="row.reason === 'COMPLAINT' ? 'danger' : 'info'">
              {{ t(`emails.skipReasons.${row.reason}`) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('emails.suppressDetail')" min-width="260">
          <template #default="{ row }">
            <div class="ellipsis" :title="row.detail">{{ row.detail || '—' }}</div>
            <div class="sub">{{ shortTime(row.createdAt) }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="common('actions')" width="100">
          <template #default="{ row }">
            <el-button v-if="canSuppress" link type="danger" @click="doUnsuppress(row)">
              {{ t('emails.unsuppress') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <!-- Cursor paging: 上一页 / 下一页 only, no page numbers. A jump to
           page 40 has no meaning when pages are positions in a list that
           grows at the top — Gmail's pager for the same reason. -->
      <div v-if="isKeysetView && (total > 0 || cursorStack.length)" class="pager keyset">
        <span class="sub">{{ t('emails.totalMails', { n: total }) }}</span>
        <el-button size="small" :disabled="!cursorStack.length" @click="prevPage">
          {{ t('emails.prevPage') }}
        </el-button>
        <el-button size="small" :disabled="!nextCursor" @click="nextPage">
          {{ t('emails.nextPage') }}
        </el-button>
      </div>
      </template>
    </section>

    <EmailComposer ref="composer" v-model="composing" @sent="onSent" @saved="onDraftSaved" />

    <el-dialog v-model="requeueOpen" :title="t('emails.requeueTitle')" width="480px">
      <p class="hint">{{ t('emails.requeueHint') }}</p>
      <el-form label-width="90px">
        <el-form-item :label="t('emails.oldAddress')">
          <span class="mono">{{ requeueRow?.toEmail }}</span>
        </el-form-item>
        <el-form-item :label="t('emails.newAddress')">
          <el-input v-model="newEmail" :placeholder="t('emails.newAddressPlaceholder')" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="requeueOpen = false">{{ common('cancel') }}</el-button>
        <el-button type="primary" :loading="acting" @click="doRequeue">
          {{ common('confirm') }}
        </el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="suppressOpen" :title="t('emails.addSuppression')" width="480px">
      <el-form label-width="90px">
        <el-form-item :label="t('emails.email')">
          <el-input v-model="suppressForm.email" />
        </el-form-item>
        <el-form-item :label="t('emails.suppressReason')">
          <el-select v-model="suppressForm.reason" style="width: 100%">
            <el-option :label="t('emails.skipReasons.UNSUBSCRIBE')" value="UNSUBSCRIBE" />
            <el-option :label="t('emails.skipReasons.COMPLAINT')" value="COMPLAINT" />
            <el-option :label="t('emails.skipReasons.MANUAL')" value="MANUAL" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('emails.suppressDetail')">
          <el-input v-model="suppressForm.detail" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="suppressOpen = false">{{ common('cancel') }}</el-button>
        <el-button type="primary" :loading="acting" @click="doSuppress">
          {{ common('confirm') }}
        </el-button>
      </template>
    </el-dialog>
  </div>
  <!-- Outside the locked/unlocked branches: an administrator may need the
       host settings before anybody can sign in at all. -->
  <MailHostDialog v-model="hostOpen" />

  <!-- Preview. Rendered from the storage origin rather than ours, so the file
       cannot reach this page's session even if it tries — and only images and
       PDFs are ever given a preview URL in the first place. -->
  <el-dialog
    v-model="previewOpen"
    :title="previewing?.fileName"
    width="min(1000px, 92vw)"
    top="4vh"
    append-to-body
  >
    <img
      v-if="previewing && isImage(previewing)"
      :src="previewing.previewUrl"
      :alt="previewing.fileName"
      class="preview-img"
    />
    <iframe
      v-else-if="previewing"
      :src="previewing.previewUrl"
      class="preview-frame"
      :title="previewing.fileName"
    />
    <template #footer>
      <a
        v-if="previewing?.downloadUrl"
        class="el-button el-button--primary"
        :href="previewing.downloadUrl"
        :download="previewing.fileName"
      >
        {{ t('emails.download') }}
      </a>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter, type LocationQuery } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { del, get, http, mailHostRequest, post } from '../api'
import { onLive } from '../live'
import { useAuthStore } from '../stores/auth'
import EmailComposer from '../components/EmailComposer.vue'
import MailReader, { type Mail } from '../components/MailReader.vue'
import MailboxGate from '../components/MailboxGate.vue'
import MailHostDialog from '../components/MailHostDialog.vue'
import MailList from '../components/MailList.vue'
import {
  Box,
  CircleClose,
  Download,
  Paperclip,
  View,
  Clock,
  Delete,
  EditPen,
  Message,
  Promotion,
  Star,
  Warning,
  WarningFilled,
} from '@element-plus/icons-vue'
// Shared mail-surface tokens. Global rather than scoped: the list is its own
// component, and the two have to agree on density or it reads as accidental.
import '../styles/mailbox.css'

interface InboundMail {
  id: string
  fromEmail: string
  fromName: string
  toEmail: string
  subject: string
  snippet: string
  threadKey: string
  isRead: boolean
  isStarred: boolean
  hasAttachments: boolean
  // Whether the original MIME is still archived. Only set on the detail read;
  // absent in list rows, which is why 作为附件转发 lives on the open mail.
  hasRaw?: boolean
  // Messages in this conversation; the list shows one row per conversation.
  threadCount?: number
  receivedAt: string
  sentAt: string
  bodyHtml?: string
  bodyText?: string
  attachments?: {
    id: string
    fileName: string
    contentType: string
    fileSize: string
    // Signed and short-lived; empty when the bytes were never stored.
    downloadUrl?: string
    // Whether a copy was kept at all. Without it, a link missing because
    // something is broken looks exactly like a file that never existed.
    stored?: boolean
    // Present only for what can be shown inline: images and PDF.
    previewUrl?: string
  }[]
}

interface Suppression {
  id: string
  email: string
  reason: string
  detail: string
  createdAt: string
}

const { t, locale } = useI18n()
const common = (k: string) => t(`common.${k}`)
const auth = useAuthStore()
const canWrite = computed(() => auth.can('mail:email:write'))
const canSuppress = computed(() => auth.can('mail:suppression:write'))
const isAdmin = computed(() => auth.can('iam:role:write'))

// An icon per folder. Text alone made the rail a list of similar-length words
// that had to be read; the icon is what the eye actually navigates by once the
// positions are learned.
const folders = [
  { key: 'inbox', icon: Message },
  { key: 'starred', icon: Star },
  { key: 'drafts', icon: EditPen },
  { key: 'scheduled', icon: Clock },
  { key: 'sent', icon: Promotion },
  { key: 'attention', icon: WarningFilled },
  { key: 'archive', icon: Box },
  { key: 'junk', icon: Warning },
  { key: 'trash', icon: Delete },
  { key: 'suppressions', icon: CircleClose },
]
// The inbox is the landing folder now that mail actually arrives in it.
const folder = ref('inbox')
// The four slices of the same mailbox: what differs is only the filter the
// server applies, so they share one table, one search box and one pager.
const INBOUND_VIEWS: Record<string, string> = {
  inbox: 'INBOX',
  starred: 'STARRED',
  archive: 'ARCHIVE',
  junk: 'JUNK',
  trash: 'TRASH',
}
const isInboundView = computed(() => folder.value in INBOUND_VIEWS)
// Every mailbox folder pages by cursor. A page number is meaningless on a
// list that grows at the top, and 已发送 grows at the top like the rest.
const isKeysetView = computed(
  () => isInboundView.value || folder.value === 'sent' || folder.value === 'attention' || folder.value === 'scheduled',
)
// Junk and the trash get a delete-everything button instead: marking a spam
// folder read is housekeeping nobody wants, and in the trash it is meaningless.
const canMarkAllRead = computed(
  () => isInboundView.value && folder.value !== 'junk' && folder.value !== 'trash',
)
const searchKey = computed(() => {
  if (folder.value === 'sent') return 'sent'
  if (isInboundView.value) return 'inbox'
  return folder.value
})

const keyword = ref('')
const page = ref(1)
const pageSize = 20
const total = ref(0)
const loading = ref(false)
const messages = ref<Message[]>([])
const suppressions = ref<Suppression[]>([])
const attentionCount = ref(0)
const composing = ref(false)
const composer = ref()
const drafts = ref<Draft[]>([])
const scheduled = ref<Scheduled[]>([])
const acting = ref(false)

const inbound = ref<InboundMail[]>([])
// Which sent list is showing: what the ERP sent (with delivery status and
// open tracking) or the mailbox's own Sent folder (plain history, any client).
// One Sent list, so no view to choose. ?sent= is still parsed off the URL so
// an old bookmark does not fail; it simply no longer changes anything.
const mailboxSent = ref<SentMail[]>([])
const unreadCount = ref(0)
// Where the next inbound page starts; empty means this is the last one.
const nextCursor = ref('')
const syncing = ref(false)
const markingAll = ref(false)
const emptying = ref(false)
// What the server last said went wrong with this mailbox, empty when healthy.
const syncError = ref('')
// The mail being read full-page. Set from the URL, never directly: opening a
// mail is a navigation, so refresh reopens it and back returns to the list.
const openedInbound = ref<InboundMail | null>(null)

// ---------------------------------------------------------------- URL state
// The address bar is the source of truth for where the person is: folder,
// page, search, sent-list toggle and the opened mail all live in the query.
// Every click routes through pushState; the route watcher is the only thing
// that loads data. That single direction is what makes refresh keep the
// position and the browser's back button behave.
const route = useRoute()
const router = useRouter()

interface UrlState {
  folder: string
  page: number
  q: string
  sent: 'erp' | 'mailbox'
  mail: string
  // The outbound counterpart: one sent mail the ERP has a record of.
  msg: string
  // Where an inbound list page starts. Opaque server token; empty is the
  // first page. Offset paging (page) still drives sent/attention.
  cursor: string
}

// What the screen currently shows. null until the first applyRoute, so the
// initial navigation always loads.
let applied: UrlState | null = null

// Every folder the address bar will accept. A key missing from here does not
// fail loudly — the URL simply falls back to the inbox, and the folder looks
// like it does not work.
const FOLDER_KEYS = new Set(['inbox', 'starred', 'drafts', 'scheduled', 'sent', 'attention', 'archive', 'junk', 'trash', 'suppressions'])

function parseQuery(q: LocationQuery): UrlState {
  const one = (v: unknown) => (Array.isArray(v) ? String(v[0] ?? '') : v == null ? '' : String(v))
  const f = one(q.folder)
  const p = Number(one(q.page))
  return {
    folder: FOLDER_KEYS.has(f) ? f : 'inbox',
    page: Number.isInteger(p) && p > 1 ? p : 1,
    q: one(q.q),
    sent: one(q.sent) === 'mailbox' ? 'mailbox' : 'erp',
    mail: /^\d+$/.test(one(q.mail)) ? one(q.mail) : '',
    msg: /^\d+$/.test(one(q.msg)) ? one(q.msg) : '',
    cursor: one(q.c),
  }
}

// Defaults stay out of the address bar: /emails, not /emails?folder=inbox&page=1.
function toQuery(s: UrlState): Record<string, string> {
  const query: Record<string, string> = {}
  if (s.folder !== 'inbox') query.folder = s.folder
  if (s.page > 1) query.page = String(s.page)
  if (s.q) query.q = s.q
  if (s.folder === 'sent' && s.sent !== 'erp') query.sent = s.sent
  if (s.mail) query.mail = s.mail
  if (s.msg) query.msg = s.msg
  if (s.cursor) query.c = s.cursor
  return query
}

// The cursors of the pages walked through to reach this one, newest last.
// Keyset paging knows how to go forward, not back, so 上一页 replays the
// cursor it came from. Kept in history state rather than a component ref so
// it survives a refresh and so back/forward each restore the stack as it
// stood on that entry.
const cursorStack = ref<string[]>([])

function stackFromHistory(): string[] {
  const st = (history.state as { mailStack?: unknown } | null)?.mailStack
  return Array.isArray(st) ? (st as string[]) : []
}

function pushState(over: Partial<UrlState>, stack?: string[]) {
  const cur = applied ?? parseQuery(route.query)
  const next = { ...cur, ...over }
  // A page number and a cursor are two answers to the same question; setting
  // one has to clear the other or a stale cursor would survive a search.
  if (over.cursor === undefined && (over.folder !== undefined || over.q !== undefined || over.sent !== undefined || over.page !== undefined)) {
    next.cursor = ''
  }
  // "Back to the list" is one intent however it is spelled, and inbound and
  // outbound details occupy the same slot on screen. Clearing them together
  // here beats remembering to name all three at every call site — the kind of
  // thing that works until somebody adds a fourth.
  if (over.mail === '') {
    next.msg = ''
  }
  // Navigating to where we already are is a plain refresh, not a navigation:
  // pushing an identical route would be silently dropped by the router.
  if (applied && JSON.stringify(toQuery(next)) === JSON.stringify(toQuery(cur))) {
    load()
    return
  }
  router.push({ query: toQuery(next), state: { mailStack: stack ?? [] } })
}

// The one place the URL turns into screen state. Loads only what changed:
// arriving on a mail link fetches both the list underneath and the mail, but
// closing the mail afterwards refetches neither.
function applyRoute() {
  if (locked.value !== false) return
  if (route.path !== '/emails') return
  const s = parseQuery(route.query)
  const prev = applied
  applied = s
  cursorStack.value = stackFromHistory()
  folder.value = s.folder
  page.value = s.page
  keyword.value = s.q
  if (
    !prev ||
    prev.folder !== s.folder ||
    prev.page !== s.page ||
    prev.q !== s.q ||
    prev.sent !== s.sent ||
    prev.cursor !== s.cursor
  ) {
    load()
  }
  if (!prev || prev.mail !== s.mail) {
    if (s.mail) {
      openDetail(s.mail)
    } else {
      openedInbound.value = null
      threadItems.value = []
    }
  }
  // The outbound side, on the same terms as the inbound one: the URL says
  // what is open, and this is the only place that acts on it.
  if (!prev || prev.msg !== s.msg) {
    if (s.msg) {
      loadMessage(s.msg)
    } else {
      openMail.value = null
    }
  }
}

watch(() => route.query, applyRoute)

// The outbound detail page: one sent mail the ERP has its own record of.
const openMail = ref<Mail | null>(null)
const readerLoading = ref(false)
// Something outbound is on screen, so the list and its toolbar step aside.
const outboundOpen = computed(() => !!openMail.value)

const requeueOpen = ref(false)
const requeueRow = ref<Message | null>(null)
const newEmail = ref('')

const suppressOpen = ref(false)
const suppressForm = reactive({ email: '', reason: 'UNSUBSCRIBE', detail: '' })

// null = still asking; the page renders nothing rather than flashing the
// gate at somebody who is already unlocked.
const locked = ref<boolean | null>(null)
const hostOpen = ref(false)

onMounted(async () => {
  // Back from Google's login page. On success the fresh grant is verified
  // right away, so binding and entering the mailbox is one motion instead of
  // a redirect followed by a second button.
  const q = new URLSearchParams(location.search)
  const oauthResult = q.get('oauth')
  if (oauthResult) {
    const boundEmail = q.get('email') ?? ''
    const reason = q.get('reason') ?? ''
    // Through the router, not history.replaceState: the router must agree the
    // query is empty or the URL-state watcher would keep seeing oauth params.
    await router.replace({ query: {} })
    if (oauthResult === 'ok') {
      ElMessage.success(t('mailbox.googleOk', { email: boundEmail }))
      try {
        const resp = await http.post('/mailbox/verify', { secret: '' }, mailHostRequest)
        const data = resp.data.data as { token: string }
        localStorage.setItem('mailUnlock', data.token)
        locked.value = false
        init()
        return
      } catch {
        /* fall through: the gate appears and says why in place */
      }
    } else {
      ElMessage({
        type: 'error',
        message: t('mailbox.googleErr', { reason }),
        duration: 0,
        showClose: true,
      })
    }
  }
  try {
    const d = await get<{ unlocked: boolean }>('/mailbox/lock-status')
    locked.value = !d.unlocked
  } catch {
    locked.value = true
  }
  if (locked.value === false) init()
})

function onUnlocked() {
  locked.value = false
  init()
}

// The unlock expires after twelve hours, and it expires wherever the person
// happens to be — usually mid-list, with a request already in flight. The
// interceptor drops the dead token and says so here; this puts the sign-in
// gate back in place of the mailbox. Before, the page stayed as it was and
// announced the problem in a toast, which left somebody looking at a list they
// could no longer do anything with.
function onMailLocked() {
  locked.value = true
}
window.addEventListener('mail-locked', onMailLocked)
onUnmounted(() => window.removeEventListener('mail-locked', onMailLocked))

async function lockMailbox() {
  try {
    await post('/mailbox/lock')
  } finally {
    // Locked locally regardless: a failed revoke call must not leave the
    // screen open while the person walks away believing it is shut.
    localStorage.removeItem('mailUnlock')
    locked.value = true
  }
}

function init() {
  applied = null
  applyRoute()
  checkSyncHealth()
  // Opening the mailbox pulls once rather than waiting up to two minutes for
  // the next poll. Somebody who just told a customer "resend it" opens this
  // page to look, and "already up to date" is the answer they need it to be.
  syncOnOpen()
  refreshAttentionCount()
  loadDraftCount()
  refreshUnread()
  // Ask once, from the mailbox itself — this is the page whose news the
  // desktop notification carries, so the browser's prompt makes sense here.
  // Shell.vue does the actual notifying, and only when the person is away.
  if ('Notification' in window && Notification.permission === 'default') {
    Notification.requestPermission()
  }
}

// New mail is pushed over the same SSE stream the rest of the app uses: the
// IMAP sync stores it, publishes a hint, and every open tab of the owner's
// hears it here. The re-fetch goes through the normal API, so this is only
// ever "go and look", never data.
onUnmounted(
  onLive((e) => {
    if (e.type !== 'mail.inbound') return
    // Not while reading a mail: yanking the list from under the detail page
    // would be invisible, and the unread badge covers the news.
    if (folder.value === 'inbox' && !openedInbound.value) {
      load()
    } else {
      refreshUnread()
    }
  }),
)

// The rail badge has to be current whichever folder is open, so it has its
// own cheap fetch rather than riding on the inbox list load.
async function refreshUnread() {
  try {
    const d = await get<{ unreadCount: number }>('/inbound-mails', { page: 1, page_size: 1 })
    unreadCount.value = Number(d.unreadCount ?? 0)
  } catch {
    /* the badge going stale is not worth an error toast */
  }
}

// The rail shows a count, so it has to be current whichever folder is open.
async function loadDraftCount() {
  try {
    const d = await get<{ drafts: Draft[] }>('/email-drafts')
    drafts.value = d.drafts ?? []
  } catch {
    drafts.value = []
  }
}

async function openDraft(row: Draft) {
  composing.value = true
  // Wait for the dialog to mount before handing it the draft, or the watch
  // that resets a fresh compose would wipe what we just loaded.
  await nextTick()
  await composer.value?.openDraft(row.id)
}

function onDraftSaved() {
  loadDraftCount()
  if (folder.value === 'drafts') load()
}

async function dropDraft(row: Draft) {
  await ElMessageBox.confirm(
    t('emails.dropDraftHint', { s: row.subject || t('emails.noSubject') }),
    common('delete'),
    { type: 'warning' },
  )
  await del(`/email-drafts/${row.id}`)
  ElMessage.success(t('emails.draftDropped'))
  load()
  loadDraftCount()
}

// A scheduled send is still the sender's to change. Both actions race the
// worker on purpose — it may have claimed the mail a second ago — so the
// server's answer, not the button, decides what the person is told.
async function sendScheduledNow(row: Scheduled) {
  const d = await post<{ released: number }>(`/email-scheduled/${row.campaignId}/send-now`)
  ElMessage.success(t('emails.sendNowDone', { n: d.released }))
  load()
}

async function cancelScheduled(row: Scheduled) {
  await ElMessageBox.confirm(
    t('emails.cancelScheduleAsk'),
    t('emails.cancelSchedule'),
    { type: 'warning' },
  )
  await post(`/email-scheduled/${row.campaignId}/cancel`)
  ElMessage.success(t('emails.cancelScheduleDone'))
  load()
  loadDraftCount()
}

// The scheduled time, on the reader's own clock. shortTime slices the string
// and would show UTC, which is the one reading nobody scheduled by.
function localTime(v: string) {
  if (!v) return ''
  const at = new Date(v)
  if (Number.isNaN(at.getTime())) return shortTime(v)
  return new Intl.DateTimeFormat(locale.value, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(at)
}

// "in 3 days", "3天后" — the unit the gap deserves, in the reader's language.
function untilText(v: string) {
  const at = new Date(v).getTime()
  if (Number.isNaN(at)) return ''
  const rtf = new Intl.RelativeTimeFormat(locale.value, { numeric: 'auto' })
  const mins = Math.round((at - Date.now()) / 60000)
  if (Math.abs(mins) < 60) return rtf.format(mins, 'minute')
  const hours = Math.round(mins / 60)
  if (Math.abs(hours) < 24) return rtf.format(hours, 'hour')
  return rtf.format(Math.round(hours / 24), 'day')
}

function switchFolder(key: string) {
  // Clear the box too, or clicking the current folder with an unsearched
  // keyword sitting in it would silently search for it.
  keyword.value = ''
  pushState({ folder: key, page: 1, q: '', mail: '' })
}

function reload() {
  pushState({ page: 1, q: keyword.value, mail: '' })
}

// Inbound lists page by cursor: forward hands back the token the server
// returned, back replays the one this page was reached with. Both are
// navigations, so the address bar and the browser's own buttons stay honest.
function nextPage() {
  if (!nextCursor.value) return
  pushState({ cursor: nextCursor.value, mail: '' }, [...cursorStack.value, applied?.cursor ?? ''])
}

function prevPage() {
  const stack = cursorStack.value
  if (!stack.length) return
  pushState({ cursor: stack[stack.length - 1], mail: '' }, stack.slice(0, -1))
}

function backToList() {
  pushState({ mail: '' })
}

async function load() {
  loading.value = true
  try {
    if (isInboundView.value) {
      const d = await get<{
        mails: InboundMail[]
        meta: { total: string }
        unreadCount: number
        nextCursor: string
      }>('/inbound-mails', {
        page_size: pageSize,
        keyword: keyword.value,
        view: INBOUND_VIEWS[folder.value],
        cursor: applied?.cursor ?? '',
      })
      inbound.value = d.mails ?? []
      total.value = Number(d.meta?.total ?? 0)
      unreadCount.value = Number(d.unreadCount ?? 0)
      nextCursor.value = d.nextCursor ?? ''
    } else if (folder.value === 'drafts') {
      const d = await get<{ drafts: Draft[] }>('/email-drafts')
      drafts.value = d.drafts ?? []
    } else if (folder.value === 'scheduled') {
      const d = await get<{
        sends: Scheduled[]
        meta: { total: string }
        nextCursor: string
      }>('/email-scheduled', {
        page_size: pageSize,
        cursor: applied?.cursor ?? '',
      })
      scheduled.value = d.sends ?? []
      total.value = Number(d.meta?.total ?? 0)
      nextCursor.value = d.nextCursor ?? ''
    } else if (folder.value === 'sent') {
      const d = await get<{
        mails: SentMail[]
        meta: { total: string }
        nextCursor: string
      }>('/mailbox-sent', {
        page_size: pageSize,
        keyword: keyword.value,
        cursor: applied?.cursor ?? '',
      })
      mailboxSent.value = d.mails ?? []
      total.value = Number(d.meta?.total ?? 0)
      nextCursor.value = d.nextCursor ?? ''
    } else if (folder.value === 'attention') {
      const d = await get<{
        messages: Message[]
        meta: { total: string }
        nextCursor: string
      }>('/email-messages', {
        page_size: pageSize,
        keyword: keyword.value,
        attention_only: true,
        cursor: applied?.cursor ?? '',
      })
      messages.value = d.messages ?? []
      total.value = Number(d.meta?.total ?? 0)
      nextCursor.value = d.nextCursor ?? ''
      attentionCount.value = total.value
    } else {
      const d = await get<{ suppressions: Suppression[] }>('/email-suppressions', {
        keyword: keyword.value,
      })
      suppressions.value = d.suppressions ?? []
    }
  } finally {
    loading.value = false
  }
}

// A row click is a navigation; the route watcher does the fetching.
function openInbound(row: InboundMail) {
  pushState({ mail: row.id })
}

// Fetches the mail named in the URL. Opening marks it read server-side; the
// row (when the list is loaded) and the badge follow.
async function openDetail(id: string) {
  try {
    const d = await get<{ mail: InboundMail }>(`/inbound-mails/${id}`)
    openedInbound.value = d.mail
    const row = inbound.value.find((r) => r.id === id)
    if (row && !row.isRead) {
      row.isRead = true
      unreadCount.value = Math.max(0, unreadCount.value - 1)
    }
    loadThread(d.mail)
  } catch {
    // A dead link — deleted mail, somebody else's id — falls back to the
    // list rather than a blank page.
    pushState({ mail: '' })
  }
}

// The conversation around it, fetched after the mail itself is already on
// screen. Only a real exchange (more than this one message) switches the
// page into thread mode; the opened mail arrives expanded, history
// collapsed to one line each.
async function loadThread(mail: InboundMail) {
  threadItems.value = []
  expandedThread.value = new Set()
  if (!mail.threadKey) return
  try {
    const tr = await get<{ items: ThreadItem[] }>('/mail-threads', { key: mail.threadKey })
    if ((tr.items ?? []).length > 1) {
      threadItems.value = tr.items
      expandedThread.value = new Set([`IN:${mail.id}`])
    }
  } catch {
    /* the single-mail view already covers the failure */
  }
}

interface ThreadItem {
  direction: string
  id: string
  subject: string
  body: string
  bodyFormat: string
  counterparty: string
  who: string
  at: string
}
const threadItems = ref<ThreadItem[]>([])
const expandedThread = ref<Set<string>>(new Set())

function isThreadOpen(it: ThreadItem) {
  return expandedThread.value.has(`${it.direction}:${it.id}`)
}

function toggleThreadItem(it: ThreadItem) {
  const k = `${it.direction}:${it.id}`
  const next = new Set(expandedThread.value)
  if (next.has(k)) {
    next.delete(k)
  } else {
    next.add(k)
  }
  expandedThread.value = next
}

// Housekeeping straight from a list row, without opening the mail. Each of
// these also reaches the mail host within seconds; see the write-back queue.
//
// Read/unread updates the row in place — the list must not jump out from
// under the cursor for a change that only alters how the row looks. Anything
// that moves the mail to another folder reloads, because the row is leaving.
// ------------------------------------------------------------ selection ---
// The ids ticked in the list. Held here rather than in MailList because the
// toolbar acts on them and a reload re-renders the list.
const picked = ref<string[]>([])
const bulkBusy = ref(false)

// Only rows still on screen count. A selection that survived a folder change
// or a page turn would act on mail the person can no longer see.
const pickedRows = computed(() => inbound.value.filter((m) => picked.value.includes(m.id)))
const allPicked = computed(
  () => inbound.value.length > 0 && pickedRows.value.length === inbound.value.length,
)
const somePicked = computed(
  () => pickedRows.value.length > 0 && pickedRows.value.length < inbound.value.length,
)

function toggleAllPicked() {
  // Partial counts as "on" for this purpose: with some ticked, the obvious
  // meaning of clicking the box is "never mind", not "and the rest too".
  picked.value = allPicked.value || somePicked.value ? [] : inbound.value.map((m) => m.id)
}

// A new list means a new set of things to choose from.
watch([folder, () => inbound.value], () => {
  if (picked.value.length) picked.value = []
})

// Applies one change to everything ticked.
//
// One request per mail, deliberately: the server coalesces them where it
// counts — every queued flag change for the same account and folder leaves as
// a single IMAP STORE — so a batch endpoint would save round trips to our own
// gateway and nothing at the mail host. Sent a few at a time so twenty
// selected mails do not open twenty connections at once.
async function bulkMark(flags: Record<string, boolean>) {
  const rows = pickedRows.value
  if (!rows.length) return
  bulkBusy.value = true
  try {
    await inChunks(rows, (row) =>
      post(`/inbound-mails/${row.id}/mark`, { ...flags, wholeThread: true }),
    )
    picked.value = []
    load()
    refreshUnread()
  } finally {
    bulkBusy.value = false
  }
}

async function bulkPurge() {
  const rows = pickedRows.value
  if (!rows.length) return
  await ElMessageBox.confirm(
    t('emails.purgeManyHint', { n: rows.length }),
    t('emails.purge'),
    { type: 'warning', confirmButtonText: t('emails.purge') },
  )
  bulkBusy.value = true
  try {
    await inChunks(rows, (row) => del(`/inbound-mails/${row.id}?whole_thread=true`))
    picked.value = []
    load()
  } finally {
    bulkBusy.value = false
  }
}

const bulkConcurrency = 4

async function inChunks<T>(rows: T[], run: (row: T) => Promise<unknown>) {
  for (let i = 0; i < rows.length; i += bulkConcurrency) {
    await Promise.all(rows.slice(i, i + bulkConcurrency).map(run))
  }
}

async function markRow(row: InboundMail, flags: Record<string, boolean>) {
  await post(`/inbound-mails/${row.id}/mark`, { ...flags, wholeThread: true })
  if ('read' in flags) {
    row.isRead = flags.read
    refreshUnread()
    return
  }
  load()
  refreshUnread()
}

async function purgeRow(row: InboundMail) {
  await ElMessageBox.confirm(t('emails.purgeHint'), t('emails.purge'), {
    type: 'warning',
    confirmButtonText: t('emails.purge'),
  })
  await del(`/inbound-mails/${row.id}?whole_thread=true`)
  ElMessage.success(t('emails.purged'))
  load()
}

async function toggleStar(row: InboundMail) {
  // Optimistic: a star that waits for the network feels broken.
  row.isStarred = !row.isStarred
  try {
    // Whole conversation, like every other action on a list row: the row's
    // star reads as "starred if any message is", so unstarring has to clear
    // them all or the star would come straight back on the next load.
    await post(`/inbound-mails/${row.id}/mark`, {
      starred: row.isStarred,
      wholeThread: true,
    })
  } catch {
    row.isStarred = !row.isStarred
  }
  if (folder.value === 'starred' && !row.isStarred) load()
}

// The detail-page actions: mark unread, archive/unarchive, trash/restore.
// Each navigates back to the list and reloads — the mail just left this view.
//
// Applied to the whole conversation, because that is what this page shows and
// what the list row stands for: archiving here has to move the exchange, not
// leave it in the inbox one message lighter.
async function markOpened(flags: Record<string, boolean>) {
  if (!openedInbound.value) return
  await post(`/inbound-mails/${openedInbound.value.id}/mark`, {
    ...flags,
    wholeThread: true,
  })
  pushState({ mail: '' })
  load()
  refreshUnread()
}

// Permanent deletion, trash only, and permanent on both sides: the ERP copy
// and the mail host's original go together. The confirm says so, because this
// is the one action nobody can check afterwards.
async function purgeOpened() {
  if (!openedInbound.value) return
  await ElMessageBox.confirm(t('emails.purgeHint'), t('emails.purge'), {
    type: 'warning',
    confirmButtonText: t('emails.purge'),
  })
  // Whole conversation, matching the trash list's one-row-per-conversation.
  await del(`/inbound-mails/${openedInbound.value.id}?whole_thread=true`)
  ElMessage.success(t('emails.purged'))
  pushState({ mail: '' })
  load()
}

// Reply and forward open the composer over the detail page — cancelling it
// lands back on the mail, not on the list. The composer prefills
// recipient/subject/quote; the server does the threading (reply) and carries
// the original's attachments (forward).
async function replyToInbound() {
  if (!openedInbound.value) return
  composing.value = true
  await nextTick()
  composer.value?.openReply(openedInbound.value)
}

async function forwardInbound() {
  if (!openedInbound.value) return
  composing.value = true
  await nextTick()
  composer.value?.openForward(openedInbound.value)
}

// Forwards the original message itself, as a .eml file, instead of our
// rendering of it. Guarded on hasRaw as well as disabling the menu item: the
// server refuses without the archived MIME, and a disabled control is a hint,
// not an enforcement.
async function forwardInboundAsAttachment() {
  if (!openedInbound.value?.hasRaw) return
  composing.value = true
  await nextTick()
  composer.value?.openForwardAsAttachment(openedInbound.value)
}

// Empties the trash in one go: every mail in it, permanently. Says the number
// out loud in the confirm — "12 封" is a different decision from "2 封", and
// the button cannot know which one somebody thinks they are making.
//
// The confirm asks about the mail, not about the plumbing. That deletion also
// reaches the mail host is true and is what anybody would expect of a mailbox;
// spelling it out turned a yes-or-no question into a paragraph about how the
// system is built.
async function emptyTrash() {
  await ElMessageBox.confirm(
    t('emails.emptyTrashHint', { n: total.value }),
    t('emails.emptyTrash'),
    { type: 'warning', confirmButtonText: t('emails.emptyTrash') },
  )
  emptying.value = true
  try {
    await post<{ deleted: number }>('/inbound-mails/empty-trash', undefined, mailHostRequest)
    // No toast. The trash emptying in front of them is the confirmation.
    load()
  } finally {
    emptying.value = false
  }
}

// Clears the junk folder into the trash. Same confirm shape as emptying the
// trash — the number is in the question, because "3 封" and "300 封" are not
// the same decision — but the outcome is recoverable, and the wording says so.
async function emptyJunk() {
  await ElMessageBox.confirm(
    t('emails.emptyJunkHint', { n: total.value }),
    t('emails.emptyJunk'),
    { type: 'warning', confirmButtonText: t('emails.emptyJunk') },
  )
  emptying.value = true
  try {
    await post<{ deleted: number }>('/inbound-mails/empty-junk', undefined, mailHostRequest)
    load()
    refreshUnread()
  } finally {
    emptying.value = false
  }
}

// Marks the current view read, immediately.
//
// No confirm. This asked one until now, on the reasoning that unread is a
// to-do list — but a confirm is for a decision that cannot be walked back, and
// this one can: every mail is still there, and any of them can be marked
// unread again. Standing between somebody and a button they press daily is a
// worse cost than the mistake it prevents.
async function markAllRead() {
  markingAll.value = true
  try {
    const d = await post<{ marked: number }>(
      `/inbound-mails/mark-view-read?view=${INBOUND_VIEWS[folder.value]}`,
    )
    ElMessage.success(t('emails.markedAllRead', { n: d.marked ?? 0 }))
    load()
    refreshUnread()
  } finally {
    markingAll.value = false
  }
}

// Reads the account's recorded health. The credential can be dead while the
// unlock token is still valid — one is "may this browser see the mailbox",
// the other is "does the mailbox still answer" — so the gate letting somebody
// in says nothing about whether mail is still arriving.
async function checkSyncHealth() {
  try {
    const d = await get<{ account: { lastError: string } }>('/my-mail-account')
    syncError.value = d.account?.lastError ?? ''
  } catch {
    /* the banner is a courtesy; its absence must not break the page */
  }
}

// The banner has to be able to go away on its own.
//
// It used to be read once, on mount. So a problem that had since been fixed —
// by signing in again, by the host coming back, by the poller simply
// succeeding — kept shouting until somebody reloaded the page, and a mailbox
// that was working looked broken. This is a mailbox: it is left open all day,
// which is exactly the window in which both the breaking and the mending
// happen.
//
// One cheap read a minute, and only while the page is actually being looked
// at — a backgrounded tab has nobody to inform.
const healthEvery = 60_000
const healthTimer = window.setInterval(() => {
  if (locked.value === false && document.visibilityState === 'visible') checkSyncHealth()
}, healthEvery)
onUnmounted(() => window.clearInterval(healthTimer))

// A quiet pull on open: no spinner, no toast. A failure shows up as the
// banner, which is where a persistent problem belongs — not in a toast that
// disappears before it is read.
async function syncOnOpen() {
  try {
    const d = await post<{ fetched: number; detail: string }>(
      '/mailbox/sync',
      undefined,
      mailHostRequest,
    )
    if (d.detail) {
      syncError.value = d.detail
      return
    }
    syncError.value = ''
    if ((d.fetched ?? 0) > 0 && !openedInbound.value) load()
  } catch {
    /* the poller keeps trying; checkSyncHealth reports what it finds */
  }
}

// The one repair that fits in a button: sign in to the mailbox again. For an
// OAuth account this is the Google round trip; for a password account the
// gate is the place to retype the code, so this locks and shows it.
async function reauth() {
  try {
    const d = await get<{ account: { authKind: string } }>('/my-mail-account')
    if (d.account?.authKind === 'OAUTH') {
      // Full-page departure, same as the gate: popups get blocked, and
      // Google's page is where the person should see themselves go.
      const r = await get<{ url: string }>('/oauth/google/start')
      window.location.href = r.url
      return
    }
  } catch {
    /* fall through to the gate, which can handle either kind */
  }
  await lockMailbox()
}

// Fetching mail is background work, even when a person asked for it.
//
// The button used to end by navigating: back to page one of the list, with the
// open mail closed. Since a sync can take the better part of a minute, that
// regularly landed on somebody who had clicked 立即收信 and then started
// reading — and threw them out of the mail mid-sentence. Nothing here moves
// the page any more. New mail appears in the list underneath and in the badge;
// the person decides when to look at it.
//
// No success toast either: the arriving mail is the news, and a green bar
// saying "收到 0 封新邮件" is a notification about nothing.
async function syncNow() {
  syncing.value = true
  try {
    const d = await post<{ fetched: number; detail: string }>(
      '/mailbox/sync',
      undefined,
      mailHostRequest,
    )
    if (d.detail) {
      syncError.value = d.detail
      ElMessage({ type: 'error', message: d.detail, duration: 0, showClose: true })
      return
    }
    syncError.value = ''
    // The list refreshes in place, under the mail if one is open — and that
    // mail's own thread with it, so a reply that just arrived joins the
    // conversation being read rather than waiting for a reopen.
    load()
    if (openedInbound.value) loadThread(openedInbound.value)
    refreshUnread()
  } finally {
    syncing.value = false
  }
}

function humanSize(bytes: number) {
  if (!bytes) return '0 B'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
}

// The badge is what tells somebody there is work waiting, so it refreshes
// independently of whichever folder happens to be open.
async function refreshAttentionCount() {
  const d = await get<{ meta: { total: string } }>('/email-messages', {
    page: 1,
    page_size: 1,
    attention_only: true,
  })
  attentionCount.value = Number(d.meta?.total ?? 0)
}

function onSent() {
  refreshAttentionCount()
  loadDraftCount()
  // Pull straight away rather than waiting for the two-minute poll. The mail
  // becomes a real message in 已发送 only once the host's copy is fetched, and
  // until then it is a delivery record with no star and no actions. Asking now
  // turns that gap from minutes into a second or two.
  syncOnOpen()
  // A mail sent from inside another mail stays there, Gmail-style: the reply
  // appears in the conversation underneath. A fresh compose goes to the sent
  // folder to watch the delivery.
  if (openedInbound.value) {
    loadThread(openedInbound.value)
    return
  }
  pushState({ folder: 'sent', page: 1, q: '', sent: 'erp', mail: '' })
}

function statusType(s: string): 'success' | 'warning' | 'danger' | 'info' {
  if (s === 'DELIVERED' || s === 'ACCEPTED') return 'success'
  if (s === 'QUEUED' || s === 'SENDING') return 'info'
  if (s === 'HARD_BOUNCED' || s === 'COMPLAINED' || s === 'FAILED') return 'danger'
  return 'warning'
}

function shortTime(v: string) {
  if (!v) return ''
  return v.replace('T', ' ').replace('Z', '').slice(0, 16)
}

// A stable colour per correspondent, so the same customer looks the same every
// time. Hue only — saturation and lightness are fixed, which is what keeps a
// wall of avatars from turning into confetti.
type MailFile = NonNullable<InboundMail['attachments']>[number]

const previewOpen = ref(false)
const previewing = ref<MailFile | null>(null)

function openPreview(a: MailFile) {
  previewing.value = a
  previewOpen.value = true
}

function isImage(a: MailFile) {
  return (a.contentType || '').toLowerCase().startsWith('image/')
}

// Why an attachment cannot be downloaded, said accurately.
//
// These two were one message once, and it told somebody their 8 MB deck had
// no copy kept when the file was fine and a stale service was dropping the
// field. A message that confident about somebody's data has to be earned.
function fileHint(a: { fileName: string; downloadUrl?: string; stored?: boolean }) {
  if (a.downloadUrl) return t('emails.downloadFile', { f: a.fileName })
  return a.stored ? t('emails.fileUnavailable') : t('emails.fileGone')
}

function avatarStyle(email: string) {
  let h = 0
  for (const ch of email || '') h = (h * 31 + ch.charCodeAt(0)) % 360
  return { background: `hsl(${h} 55% 42%)` }
}

function initialOf(name: string) {
  return (name || '?').trim().charAt(0).toUpperCase()
}

// A bulk send has N copies of one mail. Opening it shows the mail — read from
// the first recipient's copy, which is the only place the rendered text
// exists — and lists the recipients underneath.
//
// A click is a navigation; the route watcher does the fetching. Same as the
// inbox: a sent mail is a thing you can link to, refresh on, and back out of.
// One list, two kinds of row. The ERP's own record opens the page that knows
// about delivery and opens; a copy from the host's Sent folder opens the
// ordinary mail page, because that is all there is to show about it.
function openSentRow(row: SentMail) {
  if (row.kind === 'ERP') {
    pushState({ msg: row.id })
    return
  }
  pushState({ mail: row.id })
}

function openMessage(row: Message) {
  pushState({ msg: row.id })
}

function backFromOutbound() {
  pushState({ mail: '' })
}

async function loadMessage(id: string) {
  readerLoading.value = true
  try {
    const d = await get<{ message: Mail }>(`/email-messages/${id}`)
    openMail.value = d.message
  } finally {
    readerLoading.value = false
  }
}

function openRequeue(row: Message) {
  requeueRow.value = row
  newEmail.value = ''
  requeueOpen.value = true
}

async function doRequeue() {
  if (!requeueRow.value) return
  acting.value = true
  try {
    await post(`/email-messages/${requeueRow.value.id}/requeue`, { newEmail: newEmail.value })
    ElMessage.success(t('emails.requeued'))
    requeueOpen.value = false
    load()
    refreshAttentionCount()
  } finally {
    acting.value = false
  }
}

async function doAbandon(row: Message) {
  const { value } = await ElMessageBox.prompt(t('emails.abandonHint'), t('emails.abandonTitle'), {
    inputPlaceholder: t('emails.abandonReason'),
    inputPattern: /\S/,
    inputErrorMessage: t('emails.abandonReason'),
  })
  await post(`/email-messages/${row.id}/abandon`, { reason: value })
  ElMessage.success(t('emails.abandoned'))
  load()
  refreshAttentionCount()
}

function openSuppress() {
  suppressForm.email = ''
  suppressForm.reason = 'UNSUBSCRIBE'
  suppressForm.detail = ''
  suppressOpen.value = true
}

async function doSuppress() {
  acting.value = true
  try {
    await post('/email-suppressions', { ...suppressForm })
    ElMessage.success(t('emails.suppressed'))
    suppressOpen.value = false
    load()
  } finally {
    acting.value = false
  }
}

async function doUnsuppress(row: Suppression) {
  await ElMessageBox.confirm(t('emails.unsuppressHint', { e: row.email }), t('emails.unsuppress'), {
    type: 'warning',
  })
  await del(`/email-suppressions?email=${encodeURIComponent(row.email)}`)
  ElMessage.success(t('emails.unsuppressed'))
  load()
}
</script>

<style scoped>
.mailbox {
  display: flex;
  gap: 18px;
  align-items: flex-start;
}
.rail {
  flex: none;
  width: 178px;
  position: sticky;
  top: 12px;
}
.compose {
  width: 100%;
  margin-bottom: 14px;
}
.folder {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  height: var(--mail-row-h);
  padding: 0 12px;
  margin-bottom: 2px;
  background: none;
  border: none;
  /* The capsule, cut flat against the rail's edge — the shape says "this
     column continues off-screen" rather than "here is a floating chip". */
  border-radius: var(--mail-pill);
  font-size: var(--mail-text);
  color: var(--el-text-color-regular);
  cursor: pointer;
  text-align: left;
  transition: background var(--mail-fast) var(--mail-ease);
}
.folder .fname {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ficon {
  flex: none;
  font-size: 16px;
  color: var(--el-text-color-secondary);
}
.folder.on .ficon {
  color: inherit;
}
.folder:focus-visible {
  outline: 2px solid var(--el-color-primary);
  outline-offset: -2px;
}
/* Grey under the cursor — a plain "you are pointing at this", distinct from
   the blue capsule that means "you are here". Two different statements should
   not be made in the same colour. */
.folder:hover {
  background: var(--mail-hover);
}
.folder.on {
  background: var(--el-color-primary-light-9);
  color: var(--el-color-primary);
  font-weight: 600;
}
/* The current folder keeps its own colour when pointed at: greying it would
   read as if the selection had been lost. */
.folder.on:hover {
  background: var(--el-color-primary-light-8);
}
.rail-scope {
  margin-top: 22px;
  padding: 0 12px;
}
.rail-label {
  margin-bottom: 6px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.pane {
  flex: 1;
  min-width: 0;
  /* The list responds to the width IT has, not the window's: the app shell's
     nav and this page's rail both take a fixed slice first, so a viewport
     query would be answering a different question.

     On .pane rather than on .mailbox because inline-size containment stops an
     element contributing its content's width to its ancestors — put it on the
     page root and the shell's main column, which sizes to content, collapses
     to its padding. A flex item with min-width:0 already has a definite width
     from layout, so containing it costs nothing. */
  container-type: inline-size;
  container-name: mailbox;
}
.pane-head {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 14px;
}
.pane-head h2 {
  margin: 0;
  font-size: 19px;
}
/* The select-all box aligns with the per-row boxes below it, so the column
   reads as a column rather than as a stray control above a list. */
.pick-all {
  margin-right: -2px;
  height: 32px;
}
.pick-all :deep(.el-checkbox__label) {
  display: none;
}
.picked-n {
  font-size: 14px;
  font-weight: 500;
  color: var(--el-color-primary);
}
.grow {
  flex: 1;
}
.strong {
  font-weight: 500;
}
.sub {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.ellipsis {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.counts {
  display: flex;
  gap: 8px;
  margin-bottom: 4px;
  font-size: 12px;
}
.ok {
  color: var(--el-color-success);
}
.pend {
  color: var(--el-color-info);
}
.bad {
  color: var(--el-color-danger);
  font-weight: 600;
}
.clickable :deep(.el-table__row) {
  cursor: pointer;
}
.cnt {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.notyet {
  padding: 30px 0;
}
.notyet-text {
  max-width: 460px;
  margin: 0 auto;
  font-size: 13px;
  line-height: 1.7;
  color: var(--el-text-color-secondary);
}
/* Enough height for the spinner to have somewhere to sit while the mail
   is on its way. */
.reader-slot {
  min-height: 120px;
}
.side-title {
  margin: 26px 0 10px;
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-regular);
}
.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
}
.hint {
  margin: 0 0 14px;
  font-size: 13px;
  color: var(--el-text-color-secondary);
}
.pager {
  margin-top: 14px;
  justify-content: flex-end;
}
.pager.keyset {
  display: flex;
  align-items: center;
  gap: 10px;
}

.unread-dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--el-color-primary);
  margin-right: 6px;
  vertical-align: middle;
}
.clip {
  margin-left: 4px;
  color: var(--el-text-color-secondary);
  vertical-align: middle;
}
.tcount {
  margin-left: 5px;
  font-size: 12px;
  font-weight: 400;
  color: var(--el-text-color-secondary);
}
:deep(.unread-row) {
  font-weight: 500;
}
.detail-top {
  margin-bottom: 8px;
}
.back-btn {
  padding-left: 0;
  font-size: 13px;
  color: var(--el-text-color-secondary);
}
.in-subject {
  margin: 0 0 14px;
  /* The one thing on the page that should be read first, sized to say so. */
  font-size: 22px;
  font-weight: 400;
  line-height: 1.3;
}
.in-from {
  display: flex;
  align-items: center;
  gap: 12px;
}
.avatar {
  flex: none;
  display: grid;
  place-items: center;
  width: 34px;
  height: 34px;
  border-radius: 50%;
  color: #fff;
  font-size: 15px;
  font-weight: 500;
  user-select: none;
}
.in-who {
  min-width: 0;
}
.in-meta {
  display: flex;
  align-items: baseline;
  gap: 8px;
}
.in-when {
  white-space: nowrap;
}
.in-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 14px;
}
.thread-item {
  transition: box-shadow var(--mail-fast) var(--mail-ease);
}
.thread-item:hover {
  box-shadow: var(--mail-hover-shadow);
}
.junk-note {
  margin-bottom: 12px;
}
.sync-error {
  margin-bottom: 14px;
}
.sync-error-body {
  display: flex;
  align-items: center;
  gap: 12px;
}
.thread-count {
  margin-bottom: 8px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.thread-item {
  margin-bottom: 6px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: var(--mail-radius);
  overflow: hidden;
}
.thread-item.out {
  background: var(--el-fill-color-lighter);
}
.thread-head {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 8px 10px;
  background: none;
  border: none;
  cursor: pointer;
  text-align: left;
  font-size: 13px;
}
.thread-head:hover {
  background: var(--el-fill-color-light);
}
.thread-body {
  padding: 4px 12px 12px;
}
.star {
  font-size: 15px;
  color: var(--el-text-color-placeholder);
  cursor: pointer;
}
.star.on {
  color: var(--el-color-warning);
}
.star:hover {
  color: var(--el-color-warning);
}
/* An attachment reads as a thing you can pick up: a bordered card with the
   file's name and weight, not a coloured word. */
.files {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.file {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  max-width: 320px;
  padding: 6px 12px;
  border: 1px solid var(--el-border-color);
  border-radius: var(--mail-radius);
  color: var(--el-text-color-regular);
  text-decoration: none;
  transition: background var(--mail-fast) var(--mail-ease),
    border-color var(--mail-fast) var(--mail-ease);
}
.file:hover {
  background: var(--mail-hover);
  border-color: var(--el-color-primary);
}
.file:focus-visible {
  outline: 2px solid var(--el-color-primary);
  outline-offset: 2px;
}
/* A file whose bytes are gone still gets a row — "there was a file called
   this" is worth knowing — but it must not look clickable. */
.file.dead {
  cursor: not-allowed;
  opacity: 0.55;
}
.file .fname {
  min-width: 0;
}
/* The card is a label now; the two acts on it are the targets. */
.file {
  cursor: default;
}
.fbtn {
  display: grid;
  place-items: center;
  width: 26px;
  height: 26px;
  border: none;
  border-radius: 50%;
  background: none;
  color: var(--el-text-color-secondary);
  cursor: pointer;
  text-decoration: none;
  transition: background var(--mail-fast) var(--mail-ease),
    color var(--mail-fast) var(--mail-ease);
}
.fbtn:hover {
  background: var(--el-fill-color);
  color: var(--el-color-primary);
}
.fbtn:focus-visible {
  outline: 2px solid var(--el-color-primary);
  outline-offset: 1px;
}

.preview-img {
  display: block;
  max-width: 100%;
  max-height: 72vh;
  margin: 0 auto;
}
.preview-frame {
  display: block;
  width: 100%;
  height: 72vh;
  border: 1px solid var(--mail-divider);
  border-radius: var(--mail-radius);
}

.in-html {
  line-height: 1.6;
  word-break: break-word;
  /* Mail from the wild is laid out with fixed-width tables. Squeezing one
     into the pane crushes a cell down to a single letter per line — a column
     of "S t a t u s" reading downwards. Let it be as wide as it was written
     and scroll, which is what every mail client does. */
  overflow-x: auto;
}
.in-html :deep(img) {
  max-width: 100%;
}
/* The exception to the scroll rule: an image is safe to shrink, a table is
   not, and this keeps a 2000px hero from forcing the scrollbar on its own. */
.in-html :deep(table) {
  max-width: none;
}
.in-text {
  white-space: pre-wrap;
  word-break: break-word;
  font-family: inherit;
  margin: 0;
}

.opened {
  color: var(--el-color-success);
  font-size: 12px;
  cursor: default;
}


.rail-grow {
  flex: 1;
}
.rail-lock {
  margin-top: 14px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.rail-host {
  display: block;
  margin: 6px 0 0;
}
</style>
