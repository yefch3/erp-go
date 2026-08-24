<template>
  <MailboxGate v-if="locked === true" @unlocked="onUnlocked" @host-settings="hostOpen = true" />
  <div v-else-if="locked === false" ref="mailboxEl" class="mailbox">
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
      <!-- Beside 退出邮箱 and 邮箱设置 rather than in a settings page of its
           own: a signature is part of writing mail, not a system setting. -->
      <el-button
        v-if="auth.can('mail:email:write')"
        link
        class="rail-lock"
        @click="signaturesOpen = true"
      >
        ✍️ {{ t('menu.signatures') }}
      </el-button>
      <el-button
        v-if="auth.can('mail:email:write')"
        link
        class="rail-lock"
        @click="templatesOpen = true"
      >
        📋 {{ t('menu.templates') }}
      </el-button>
      <el-button v-if="isAdmin" link class="rail-lock" @click="hostOpen = true">
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
            <div class="sub">
              {{ t('emails.inboundTo', { to: openedInbound.toEmail }) }}
              <!-- 详情 folds the headers away rather than dropping them: a
                   reader is for reading, but "which address did this really
                   come from" has to be answerable without leaving the page. -->
              <button class="details-toggle" @click="detailsOpen = !detailsOpen">
                {{ detailsOpen ? t('emails.hideDetails') : t('emails.showDetails') }}
              </button>
            </div>
          </div>
          <span class="grow" />
          <span class="sub in-when" :title="zonedStamp(openedInbound.sentAt || openedInbound.receivedAt)">
            {{ shortTime(openedInbound.sentAt || openedInbound.receivedAt) }}
          </span>
        </div>

        <!-- 不藏在详情里：详情是折起来的，而这一条正是那种「不特意去看就不会
             知道」的事。 -->
        <div v-if="replyToMismatch" class="reply-mismatch">
          <el-icon><WarningFilled /></el-icon>
          <span>{{ t('emails.replyToMismatch', { addr: openedInbound.replyTo }) }}</span>
        </div>

        <dl v-if="detailsOpen" class="mail-details">
          <template v-for="row in detailRows" :key="row.k">
            <dt>{{ row.k }}</dt>
            <dd>{{ row.v }}</dd>
          </template>
        </dl>

        <!-- 对方是否已读, for a copy out of the Sent folder.
             Stated in three states rather than two: "not opened" and "we were
             not watching" look identical in the data — both an empty
             timestamp — and only the first says anything about the recipient.
             Claiming the second as the first would be the system inventing a
             fact about a customer. -->
        <div v-if="openedInbound.folder === 'SENT'" class="readback">
          <span class="rb-label">{{ t('reader.openedLabel') }}</span>
          <el-tooltip
            v-if="openedInbound.openedAt"
            :content="t('emails.openedHint', { at: zonedStamp(openedInbound.openedAt) })"
            placement="top"
            :show-after="0"
          >
            <span class="rb-yes">
              {{ t('emails.maybeOpened') }} · {{ shortTime(openedInbound.openedAt) }}
            </span>
          </el-tooltip>
          <el-tooltip v-else-if="openedInbound.tracked" :content="t('reader.noOpenHint')" placement="top" :show-after="0">
            <span class="rb-no">{{ t('emails.noOpenYet') }}</span>
          </el-tooltip>
          <el-tooltip v-else :content="t('reader.noTrackingHint')" placement="top" :show-after="0">
            <span class="rb-off">{{ t('reader.noTracking') }}</span>
          </el-tooltip>
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
          <!-- Taking the exchange out of the system. A split button because
               there are two errands behind one intent: print it now for the
               person standing next to you, or save the file to attach to
               something. Both go through the same audited endpoint. -->
          <el-dropdown
            v-if="canExport && openedInbound.threadKey"
            size="small"
            split-button
            :disabled="exporting"
            @click="printThread"
          >
            {{ t('emails.exportPrint') }}
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item @click="saveThread">
                  {{ t('emails.exportSave') }}
                </el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
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

          <!-- 整条会话的附件，汇总在最上面。
               每封信下面已经有自己的附件了，这一条解决的是另一个问题：业务员
               记得「客户发过一版装箱单」，却不记得在第几封信里。逐封展开去找
               是最笨的办法，而这恰恰是 ERP 该比通用邮箱强的地方。
               点一个文件就跳到它所在的那一封并展开。 -->
          <details v-if="threadFiles.length" class="thread-files-strip" open>
            <summary>{{ t('emails.threadFiles', { n: threadFiles.length }) }}</summary>
            <div class="strip-rows">
              <button
                v-for="f in threadFiles"
                :key="f.item.direction + f.item.id + ':' + f.file.id"
                type="button"
                class="strip-row"
                @click="jumpToThreadItem(f.item)"
              >
                <el-icon><Paperclip /></el-icon>
                <span class="fname ellipsis">{{ f.file.fileName }}</span>
                <span class="sub">{{ humanSize(Number(f.file.fileSize)) }}</span>
                <span class="grow" />
                <!-- 谁发的、什么时候：这两样才是「哪一封」的答案。 -->
                <span class="sub ellipsis strip-who">
                  {{ isOwnMail(f.item) ? t('emails.threadOut') : f.item.who || f.item.counterparty }}
                </span>
                <span class="sub">{{ shortTime(f.item.at) }}</span>
              </button>
            </div>
          </details>
          <div
            v-for="it in threadItems"
            :key="it.direction + it.id"
            :data-thread-item="threadItemKey(it)"
            class="thread-item"
            :class="{ out: isOwnMail(it) }"
          >
            <button type="button" class="thread-head" @click="toggleThreadItem(it)">
              <el-tag size="small" :type="isOwnMail(it) ? 'info' : 'success'" effect="plain">
                {{ isOwnMail(it) ? t('emails.threadOut') : t('emails.threadIn') }}
              </el-tag>
              <span class="strong">{{ it.who || it.counterparty }}</span>
              <span class="sub ellipsis">{{ it.counterparty }}</span>
              <span class="grow" />
              <span class="sub" :title="zonedStamp(it.at)">{{ shortTime(it.at) }}</span>
            </button>
            <div v-show="isThreadOpen(it)" class="thread-body">
              <MailBody
                v-if="it.bodyFormat === 'HTML'"
                :html="it.body"
                @selection-context="openTextExcelMenu($event, it.direction === 'IN' ? it.id : '')"
                @selection-clear="closeExcelMenu"
              />
              <pre
                v-else
                class="in-text"
                :data-mail-id="it.direction === 'IN' ? it.id : ''"
                @mouseover="onPlainTextHover"
              >{{ it.body }}</pre>
              <QuotedHistory v-if="it.quoted" :html="it.quoted" />
              <!-- 这一封自己带的附件。放在正文下面、引用历史之后，和阅读单封
                   时的顺序一致。 -->
              <MailAttachments
                v-if="it.attachments?.length"
                :files="it.attachments"
                class="thread-files"
                @preview="openPreview"
                @excel-menu="openAttachmentExcelMenu"
                @excel-hover="hoverAttachmentExcelMenu"
                @excel-leave="scheduleExcelMenuHide"
              />
            </div>
          </div>
        </template>
        <template v-else>
          <MailBody
            v-if="openedInbound.bodyHtml"
            :html="openedInbound.bodyHtml"
            @selection-context="openTextExcelMenu($event, openedInbound.id)"
            @selection-clear="closeExcelMenu"
          />
          <pre
            v-else
            class="in-text"
            :data-mail-id="openedInbound.id"
            @mouseover="onPlainTextHover"
          >{{ openedInbound.bodyText }}</pre>
          <QuotedHistory v-if="openedInbound.quotedHtml" :html="openedInbound.quotedHtml" />
        </template>
        <template v-if="openedInbound.attachments?.length">
          <el-divider />
          <h4 class="side-title">{{ t('emails.attachments') }}</h4>
          <MailAttachments
            :files="openedInbound.attachments"
            @preview="openPreview"
            @excel-menu="openAttachmentExcelMenu"
            @excel-hover="hoverAttachmentExcelMenu"
            @excel-leave="scheduleExcelMenuHide"
          />
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
          :highlight="isSearching ? keyword : ''"
          @open="openInbound"
          @star="toggleStar"
          @mark="markRow"
          @purge="purgeRow"
        />
        <el-empty
          v-if="!loading && inbound.length === 0"
          :description="
            isSearching
              ? t('emails.searchEmpty', { q: keyword })
              : t(folder === 'inbox' ? 'emails.emptyInbox' : 'emails.emptyFolder')
          "
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
            <span class="sub" :title="zonedStamp(row.updatedAt)">{{ shortTime(row.updatedAt) }}</span>
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
            <div class="sub" :title="zonedStamp(row.createdAt)">{{ shortTime(row.createdAt) }}</div>
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
  <MailSignatureDialog v-model="signaturesOpen" />
  <MailTemplatesDialog v-model="templatesOpen" />

  <!-- The selection bubble. It follows the current text selection (or a
       clicked embedded image) and leaves when the selection does. Not a
       context menu: right-click stays native, because a page cannot add an
       entry to the browser's own menu — it can only replace it, and
       replacing it costs the reader copy, look-up and translate. -->
  <div
    v-if="excelMenu.open"
    class="excel-context"
    :style="{ left: excelMenu.x + 'px', top: excelMenu.y + 'px' }"
    role="menu"
    @mouseenter="cancelExcelMenuHide"
    @mouseleave="scheduleExcelMenuHide"
  >
    <button
      type="button"
      role="menuitem"
      :disabled="(!excelAvailable && !excelMenuDirectFile) || !!excelMenu.disabledReason"
      :aria-describedby="((!excelAvailable && !excelMenuDirectFile) || excelMenu.disabledReason) ? 'excel-unavailable-reason' : undefined"
      @click="convertExcelSelection"
    >
      {{ t('emails.convertToExcel') }}
    </button>
    <p v-if="(!excelAvailable && !excelMenuDirectFile) || excelMenu.disabledReason" id="excel-unavailable-reason" class="excel-context-reason">
      {{ excelMenu.disabledReason ? t(excelMenu.disabledReason) : t('emails.excelUnavailable') }}
    </p>
  </div>

  <el-dialog
    v-model="excelOpen"
    :title="excelResult?.fileName || t('emails.excelPreview')"
    width="min(1100px, 94vw)"
    top="4vh"
    append-to-body
    :close-on-click-modal="false"
    :close-on-press-escape="false"
  >
    <div v-loading="excelBusy" class="excel-preview">
      <el-empty v-if="!excelBusy && !excelResult" :description="t('emails.excelWaiting')" />
      <template v-else-if="excelResult">
        <div class="excel-model">
          <template v-if="excelResult.model">{{ t('emails.generatedBy', { model: excelResult.model }) }}</template>
          <template v-else>{{ t('emails.excelDirectNote') }}</template>
        </div>
        <el-tabs v-model="excelSheet">
          <el-tab-pane
            v-for="sheet in excelResult.sheets"
            :key="sheet.name"
            :label="sheet.name"
            :name="sheet.name"
          >
            <p v-if="sheet.summary" class="sub">{{ sheet.summary }}</p>
            <p v-if="Number(sheet.totalRows) > sheet.rows.length" class="sub">
              {{ t('emails.excelPreviewRows', { shown: sheet.rows.length, total: sheet.totalRows }) }}
            </p>
            <div class="excel-grid">
              <table>
                <thead><tr><th v-for="c in sheet.columns" :key="c">{{ c }}</th></tr></thead>
                <tbody>
                  <tr v-for="(row, ri) in sheet.rows" :key="ri">
                    <td v-for="(cell, ci) in row.cells" :key="ci">{{ cell }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </el-tab-pane>
        </el-tabs>
      </template>
    </div>
    <template #footer>
      <el-button @click="excelOpen = false">{{ t('emails.close') }}</el-button>
      <el-button
        v-if="excelResult && auth.can('procurement:sourcing:write')"
        @click="openSourcingTransfer"
      >
        {{ t('emails.createSourcingCase') }}
      </el-button>
      <el-button v-if="excelResult" :loading="excelBusy" @click="regenerateExcel">
        {{ t('emails.regenerateExcel') }}
      </el-button>
      <el-button v-if="excelResult" type="primary" @click="downloadExcel">
        {{ t('emails.downloadExcel') }}
      </el-button>
    </template>
  </el-dialog>

  <!-- 转入采购前先落客户。客户是必填的，联系人和邮箱只是这封信的事实，
       抄过来方便核对，改不改都行。 -->
  <el-dialog
    v-model="sourcingOpen"
    :title="t('emails.sourcingTransferTitle')"
    width="min(460px, 92vw)"
    append-to-body
  >
    <p class="sourcing-hint">{{ t('emails.sourcingTransferHint') }}</p>
    <el-form label-position="top">
      <el-form-item :label="t('emails.sourcingCustomer')" required>
        <CustomerSelect
          v-model="sourcingForm.customerId"
          @selected="(customer) => (sourcingForm.customerName = customer?.name || '')"
        />
      </el-form-item>
      <el-form-item :label="t('emails.sourcingContact')">
        <el-input v-model="sourcingForm.contactName" />
      </el-form-item>
      <el-form-item :label="t('emails.sourcingContactEmail')">
        <el-input v-model="sourcingForm.contactEmail" />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="sourcingOpen = false">{{ t('emails.close') }}</el-button>
      <el-button
        type="primary"
        :loading="creatingSourcingCase"
        :disabled="!sourcingForm.customerId"
        @click="createSourcingCaseFromExcel"
      >
        {{ t('emails.createSourcingCase') }}
      </el-button>
    </template>
  </el-dialog>

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
import { del, download, get, http, mailExcelRequest, mailHostRequest, post, saveBlob } from '../api'
import { shortTime, zonedStamp } from '../lib/zonedtime'
import { isDirectTableFile, parseTableFile } from '../lib/attachmentExcel'
import { onLive } from '../live'
import { useAuthStore } from '../stores/auth'
import EmailComposer from '../components/EmailComposer.vue'
import MailReader, { type Mail } from '../components/MailReader.vue'
import MailAttachments from '../components/MailAttachments.vue'
import MailboxGate from '../components/MailboxGate.vue'
import MailHostDialog from '../components/MailHostDialog.vue'
import MailSignatureDialog from '../components/MailSignatureDialog.vue'
import MailTemplatesDialog from '../components/MailTemplatesDialog.vue'
import MailList from '../components/MailList.vue'
// Received mail renders inside a sandboxed frame. It carries the sender's own
// stylesheet now, and a stylesheet injected into this page would be a stranger
// styling the ERP — which is exactly what happened when these two sites were
// left on v-html.
import MailBody from '../components/MailBody.vue'
import QuotedHistory from '../components/QuotedHistory.vue'
import CustomerSelect from '../components/masterdata/CustomerSelect.vue'
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
  // Set only on search results: which folder the hit was found in.
  matchFolder?: string
  // Which mailbox folder this copy sits in. 'SENT' is what tells the reader
  // to show 对方是否已读 — once both are an InboundMail, nothing else does.
  folder?: string
  // 对方是否已读, for a Sent copy the ERP has a delivery record for. Three
  // states between them: openedAt set, tracked without openedAt, neither.
  openedAt?: string
  tracked?: boolean
  // The details panel.
  messageIdHeader?: string
  rawSize?: number
  // 真正的回信地址，以及收信服务器验过的两个身份。
  replyTo?: string
  cc?: string
  authSpf?: string
  authDkim?: string
  // Messages in this conversation; the list shows one row per conversation.
  threadCount?: number
  receivedAt: string
  sentAt: string
  bodyHtml?: string
  quotedHtml?: string
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
const canExport = computed(() => auth.can('mail:email:export'))

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

// Two characters, matching the server's floor. One character matches most of
// the mailbox, which is not a result set — it is the mailbox with extra steps.
// Counted in characters rather than bytes, because one Chinese character is a
// word's worth of meaning.
const isSearching = computed(
  () => isInboundView.value && [...keyword.value.trim()].length >= 2,
)
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
// The bound mailbox's own address. A thread entry whose sender is this
// address is our own mail even when it arrived through the inbox (a mail
// sent to yourself), and must wear the 我发出 tag, not 对方.
const accountEmail = ref('')

function isOwnMail(it: { direction: string; counterparty: string }) {
  return it.direction === 'OUT'
    || (accountEmail.value !== '' && it.counterparty.trim().toLowerCase() === accountEmail.value)
}
// The mail being read full-page. Set from the URL, never directly: opening a
// mail is a navigation, so refresh reopens it and back returns to the list.
const openedInbound = ref<InboundMail | null>(null)

// Folded by default, and folded again on every open: the details are for the
// one mail somebody is questioning, not a preference to carry into the next.
const detailsOpen = ref(false)
watch(openedInbound, () => {
  detailsOpen.value = false
})

// Only the rows that have something in them. An empty "抄送:" is not
// information, it is a line to read past.
// 回信地址与发件人不一致。
//
// 这不是异常，正常业务里也常见（noreply 发出、客服组统一回收）。所以措辞是
// 「注意」而不是「警告」：提示要说出事实，由看得懂的人判断，而不是替他断定
// 这是诈骗——狼来了喊多了，真来的那次就没人听了。
//
// 但它值得被看见：伪造一封看似来自老供应商的邮件、把 Reply-To 换成自己的
// 地址，是骗走货款最常用的一手，而 From 那一行看上去毫无破绽。
const replyToMismatch = computed(() => {
  const m = openedInbound.value
  if (!m?.replyTo || !m.fromEmail) return false
  return m.replyTo.trim().toLowerCase() !== m.fromEmail.trim().toLowerCase()
})

const detailRows = computed(() => {
  const m = openedInbound.value
  if (!m) return []
  const rows: { k: string; v: string }[] = []
  const add = (k: string, v?: string | number) => {
    if (v !== undefined && v !== null && v !== '' && v !== 0) rows.push({ k, v: String(v) })
  }
  add(t('emails.detail.from'), `${m.fromName ? m.fromName + ' ' : ''}<${m.fromEmail}>`)
  add(t('emails.detail.to'), m.toEmail)
  add(t('emails.detail.cc'), m.cc)
  // 只在与 From 不同时才列：一样的时候它不是信息，是一行要读过去的字。
  if (replyToMismatch.value) add(t('emails.detail.replyTo'), m.replyTo)
  add(t('emails.detail.subject'), m.subject)
  // Gmail 的 mailed-by / signed-by。空表示未记录或未通过验证 —— 两者都不该
  // 说成「验证失败」，那是在断言一件我们并不知道的事。
  add(t('emails.detail.mailedBy'), m.authSpf)
  add(t('emails.detail.signedBy'), m.authDkim)
  if (m.sentAt) add(t('emails.detail.sentAt'), zonedStamp(m.sentAt))
  if (m.receivedAt) add(t('emails.detail.receivedAt'), zonedStamp(m.receivedAt))
  add(t('emails.detail.folder'), m.folder)
  add(t('emails.detail.size'), m.rawSize ? humanSize(m.rawSize) : '')
  // Last, and unabbreviated: it is the identifier somebody quotes to a mail
  // administrator when a message has to be traced through somebody else's logs.
  add(t('emails.detail.messageId'), m.messageIdHeader)
  return rows
})

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

// Where each folder's list stood when a mail was opened, in pixels of the
// Shell's scroll column. Coming back from a mail restores it: reading one
// message must not cost the place somebody had scrolled to — page 3 of the
// inbox is an afternoon of triage. Keyed by folder (plus the search phrase,
// which is its own list), so every folder comes back to its own place.
//
// Deliberately NOT in the URL: a scroll offset is how a screen stood, not
// what it showed, and pixels shared in a link would land differently on a
// different window height anyway.
const mailboxEl = ref<HTMLElement | null>(null)
const listScroll = new Map<string, number>()

function scrollKey(s: { folder: string; q: string }): string {
  return s.q ? `${s.folder}?q=${s.q}` : s.folder
}

// The column that actually scrolls belongs to the Shell (its el-main), not
// to this page. Found by walking up rather than by naming its class, so a
// Shell layout rename cannot silently turn this feature off.
function listScroller(): HTMLElement | null {
  for (let p = mailboxEl.value?.parentElement ?? null; p; p = p.parentElement) {
    const oy = getComputedStyle(p).overflowY
    if (oy === 'auto' || oy === 'scroll') return p
  }
  return null
}

// Setting scrollTop once is not enough: at Vue's nextTick the table is in
// the DOM but el-table finishes its own height a frame later, so a restore
// aimed past the half-built height gets clamped and stays there. Retried
// across a few frames until it lands (or until the target is simply beyond
// a shrunken list, where the clamp is the right answer).
function restoreListScroll(top: number, tries = 8) {
  const sc = listScroller()
  if (!sc) return
  sc.scrollTo({ top })
  if (tries > 0 && Math.abs(sc.scrollTop - top) > 1) {
    requestAnimationFrame(() => restoreListScroll(top, tries - 1))
  }
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
  // Reading state, independent of which kind of detail fills the slot —
  // inbound and outbound clear together (pushState couples them), so the
  // transition is list↔reading, not per-field.
  const wasReading = !!(prev && (prev.mail || prev.msg))
  const nowReading = !!(s.mail || s.msg)
  if (prev && !wasReading && nowReading) {
    // The list is still on screen at this instant — the detail swaps in on
    // a later render — so this is the last honest reading of where it stood.
    listScroll.set(scrollKey(prev), listScroller()?.scrollTop ?? 0)
  }
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
  if (wasReading && !nowReading) {
    // Back to the list. The rows were never unloaded ("closing the mail
    // afterwards refetches neither"), so one tick re-renders the list at
    // full height and the saved offset means the same thing it meant when
    // it was taken. Without this the scroll lands wherever the browser
    // clamped it during the swap — which is "the top", by accident, and
    // page 3 of an afternoon's triage is gone.
    const top = listScroll.get(scrollKey(s)) ?? 0
    nextTick(() => restoreListScroll(top))
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
const signaturesOpen = ref(false)
const templatesOpen = ref(false)

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
      try {
        const resp = await http.post('/mailbox/verify', { secret: '' }, mailHostRequest)
        const data = resp.data.data as { token: string }
        localStorage.setItem('mailUnlock', data.token)
        // Said only once the person is actually through. Announcing the
        // binding first meant a green "已绑定" could sit above a red failure,
        // both true and together unreadable — the mailbox was bound and the
        // gate was still shut.
        ElMessage.success(t('mailbox.googleOk', { email: boundEmail }))
        locked.value = false
        init()
        return
      } catch {
        // Loud on purpose: the interceptor has already said what went wrong,
        // and the gate coming back with no reason was the other half of the
        // confusion.
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

// 解锁凭证是「闲置多久失效」，用着就会自动续期（服务端 UnlockStore.Check，
// 和 ERP 登录会话同一个规矩）。所以走到这里通常是真的搁了一夜，而不是
// 用着用着被踢——后者曾经是常态，因为从前的有效期从验证那一刻起算、
// 从不续期。
//
// 失效发生在人正在做别的事的时候 — usually mid-list, with a request
// already in flight. The
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
  resumeExcelJob()
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
    if (e.type === 'mail.excel_job.changed') {
      void refreshExcelJob(e.subject)
      return
    }
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

// The scheduled time, spelled out in the reader's language rather than the
// numeric shape the lists use: a send that has not happened yet is read as a
// date ("1 Mar 2026, 16:00"), not scanned as a column.
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
    if (isSearching.value) {
      // A search crosses folders, so it is a different request with a
      // different shape — not the folder list with a parameter. Gmail works
      // the same way, and for the same reason: somebody who remembers a
      // phrase does not remember where they filed it.
      const d = await get<{
        hits: { mail: InboundMail; folder: string; matchSnippet: string }[]
        meta: { total: string }
        nextCursor: string
      }>('/mail-search', {
        page_size: pageSize,
        keyword: keyword.value,
        cursor: applied?.cursor ?? '',
      })
      inbound.value = (d.hits ?? []).map((h) => ({
        ...h.mail,
        // The text around the hit replaces the opening line: showing the
        // first sentence of a mail that matched on its fourth paragraph
        // makes the result look like a mistake.
        snippet: h.matchSnippet,
        matchFolder: h.folder,
      }))
      total.value = Number(d.meta?.total ?? 0)
      nextCursor.value = d.nextCursor ?? ''
      // unreadCount is deliberately left alone: it counts the mailbox, and a
      // search is not a mailbox.
    } else if (isInboundView.value) {
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
    // Reading starts at the top by decision, not by the accident of the
    // browser clamping the list's old offset against a shorter page.
    nextTick(() => listScroller()?.scrollTo({ top: 0 }))
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
  quoted?: string
  bodyFormat: string
  counterparty: string
  who: string
  at: string
  attachments?: {
    id: string
    fileName: string
    fileSize: number | string
    contentType?: string
    downloadUrl?: string
    previewUrl?: string
    stored?: boolean
  }[]
}
const threadItems = ref<ThreadItem[]>([])

// 整条会话的附件，按时间顺序摊平。threadItems 本身就是按发生顺序来的，所以
// 这里不再排序——文件的顺序就是对话的顺序。
const threadFiles = computed(() =>
  threadItems.value.flatMap((item) =>
    (item.attachments ?? []).map((file) => ({ item, file })),
  ),
)

// 点汇总条里的文件：展开它所在的那一封并滚过去。不直接下载——使用者要找的
// 通常不只是文件，还有当时的上下文（客户说了什么、要求改哪里）。
function jumpToThreadItem(it: ThreadItem) {
  expandedThread.value = new Set([...expandedThread.value, threadItemKey(it)])
  nextTick(() => {
    document
      .querySelector(`[data-thread-item="${threadItemKey(it)}"]`)
      ?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  })
}
const expandedThread = ref<Set<string>>(new Set())

// 两条腿的行号来自不同的表，收件的 7 不是发件的 7，所以键要带方向。
function threadItemKey(it: ThreadItem) {
  return `${it.direction}:${it.id}`
}

function isThreadOpen(it: ThreadItem) {
  return expandedThread.value.has(threadItemKey(it))
}

function toggleThreadItem(it: ThreadItem) {
  const k = threadItemKey(it)
  const next = new Set(expandedThread.value)
  if (next.has(k)) {
    next.delete(k)
  } else {
    next.add(k)
  }
  expandedThread.value = next
}

// ------------------------------------------------------------------- export
// Taking the exchange out of the ERP: "send the whole negotiation to the boss
// / to the customs broker" is a real errand and today it is done by forwarding
// thirty mails one at a time.
//
// The document is built on the server, not here. Three reasons, and the third
// is the one that decided it: the same bytes have to be what the audit log
// recorded; the assembly needs both halves of the conversation, and the sent
// half is not on this page; and the transcript must not carry the sender's
// HTML, which is a judgement about what leaves the company rather than about
// how a page looks.
const exporting = ref(false)

async function fetchTranscript() {
  const key = openedInbound.value?.threadKey
  if (!key || exporting.value) return null
  exporting.value = true
  try {
    return await download('/mail-threads/export', {
      key,
      // The reader's own clock and language, which only the browser knows.
      tz: Intl.DateTimeFormat().resolvedOptions().timeZone,
      lang: locale.value,
    })
  } catch {
    // Already announced by the interceptor; a second message would say the
    // same thing twice.
    return null
  } finally {
    exporting.value = false
  }
}

async function saveThread() {
  const file = await fetchTranscript()
  if (file) saveBlob(file.blob, file.fileName)
}

async function printThread() {
  const file = await fetchTranscript()
  if (file) printDocument(await file.text())
}

// Printing through a frame of our own rather than a new tab.
//
// window.open after an await is what a popup blocker exists to stop, and the
// failure is silent — the click "does nothing". A frame is always allowed.
// It also keeps the sender's document out of a top-level page at our origin,
// which matters less here than it would with the original HTML but costs
// nothing to keep true.
function printDocument(html: string) {
  const frame = document.createElement('iframe')
  frame.setAttribute('aria-hidden', 'true')
  // Hidden, but laid out. display:none is not laid out and prints blank.
  frame.style.cssText = 'position:absolute;width:0;height:0;border:0;visibility:hidden;'
  frame.srcdoc = html
  frame.onload = () => {
    const win = frame.contentWindow
    if (!win) {
      frame.remove()
      return
    }
    win.focus()
    win.print()
    // Removing the frame while the dialog is still open cancels the job in
    // Safari, so it goes afterwards — with a timer in case a browser never
    // fires afterprint, which is the case in more of them than it should be.
    const drop = () => frame.remove()
    win.addEventListener('afterprint', drop, { once: true })
    setTimeout(drop, 120_000)
  }
  document.body.appendChild(frame)
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
    const d = await get<{ account: { lastError: string; email: string }; excelAvailable: boolean }>('/my-mail-account')
    syncError.value = d.account?.lastError ?? ''
    accountEmail.value = (d.account?.email ?? '').trim().toLowerCase()
    excelAvailable.value = d.excelAvailable === true
  } catch {
    excelAvailable.value = false
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
    const d = await post<{ fetched: number; detail: string; pending?: boolean }>(
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
    // 还在收，不是出错。一个从没同步过的邮箱首次要收几分钟，而请求前面的 nginx
    // 只等 60 秒 —— 服务端到点就先答话，这里要把它说成"进行中"而不是红字报错，
    // 否则用户会以为坏了，然后反复点，反复排队。
    if (d.pending) {
      ElMessage({ type: 'info', message: t('emails.syncPending') })
    }
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

// A stable colour per correspondent, so the same customer looks the same every
// time. Hue only — saturation and lightness are fixed, which is what keeps a
// wall of avatars from turning into confetti.
type MailFile = NonNullable<InboundMail['attachments']>[number]

interface ExcelSheet {
  name: string
  summary: string
  columns: string[]
  // 每列的模板字段标识（新版 LLM 结果携带）；没有它时退回表头映射。
  columnKeys?: string[]
  rows: { cells: string[] }[]
  totalRows: string
}

interface ExcelResult {
  fileName: string
  fileData: string
  sheets: ExcelSheet[]
  model: string
  inquiryTemplateId?: string
  inquiryTemplateCode?: string
  inquiryTemplateVersion?: number
}

interface ExcelJob {
  id: string
  status: 'PENDING' | 'PROCESSING' | 'COMPLETED' | 'FAILED'
  result?: ExcelResult
  errorCode?: string
  errorMessage?: string
  inquiryTemplateId?: string
  inquiryTemplateCode?: string
  inquiryTemplateVersion?: number
}

type ExcelSource =
  | { kind: 'text'; mailId: string; text: string }
  | { kind: 'attachment'; mailId: string; attachmentId: string }

const excelMenu = reactive({
  open: false, x: 0, y: 0, source: null as ExcelSource | null, disabledReason: '',
})
const excelOpen = ref(false)
const excelBusy = ref(false)
const excelResult = ref<ExcelResult | null>(null)
const excelJobId = ref('')
const excelSheet = ref('')
const excelAvailable = ref(false)
const creatingSourcingCase = ref(false)
const sourcingOpen = ref(false)
// 转入采购要落到一个真客户身上。邮件里只有发件人的显示名和邮箱，猜不出是
// 哪一家——客户档案的检索只认名称和编号，不认邮箱——所以让人选一次。
const sourcingForm = reactive({ customerId: '', customerName: '', contactName: '', contactEmail: '' })
const convertedExcelSource = ref<ExcelSource | null>(null)
// Results live in memory: asking for the same attachment or text again opens
// the stored workbook instead of spending another model call. 重新生成 is the
// explicit way to pay for a fresh read.
const excelResultCache = new Map<string, ExcelResult>()
let excelPollTimer: ReturnType<typeof setTimeout> | null = null

function excelCacheKey(source: ExcelSource): string {
  return source.kind === 'attachment'
    ? `attachment:${source.mailId}:${source.attachmentId}`
    : `text:${source.mailId}:${source.text}`
}

onUnmounted(() => {
  if (excelPollTimer) window.clearTimeout(excelPollTimer)
  if (excelHoverTimer) window.clearTimeout(excelHoverTimer)
  if (excelHideTimer) window.clearTimeout(excelHideTimer)
})

function positionExcelMenu(x: number, y: number, source: ExcelSource, disabledReason = '') {
  cancelExcelMenuHide()
  // x is the anchor's centre (the bubble is centred via CSS), so the clamp
  // keeps half a bubble's width inside each edge.
  excelMenu.x = Math.max(110, Math.min(x, window.innerWidth - 110))
  excelMenu.y = Math.max(8, Math.min(y, window.innerHeight - 54))
  excelMenu.source = source
  excelMenu.disabledReason = disabledReason
  excelMenu.open = true
}

// When the menu's source is a spreadsheet we can read directly, the entry
// stays available even with no model configured.
const excelMenuDirectFile = computed(() => (excelMenu.source ? directTableAttachment(excelMenu.source) : null))

let excelHoverTimer: ReturnType<typeof setTimeout> | null = null
let excelHideTimer: ReturnType<typeof setTimeout> | null = null

// Hover opens the same bubble right-click opens; the brief delay keeps a
// mouse crossing the attachments row from flashing it on every card.
function hoverAttachmentExcelMenu(event: MouseEvent, file: MailFile) {
  if (!openedInbound.value) return
  const mailId = openedInbound.value.id
  const current = excelMenu.source
  if (excelMenu.open && current?.kind === 'attachment' && current.attachmentId === file.id) return
  if (excelHoverTimer) window.clearTimeout(excelHoverTimer)
  const card = event.currentTarget as HTMLElement
  excelHoverTimer = window.setTimeout(() => {
    const rect = card.getBoundingClientRect()
    positionExcelMenu(rect.left + rect.width / 2, rect.bottom + 6, {
      kind: 'attachment', mailId, attachmentId: file.id,
    }, file.stored ? '' : 'emails.attachmentNotStored')
  }, 250)
}

function scheduleExcelMenuHide() {
  if (excelHoverTimer) {
    window.clearTimeout(excelHoverTimer)
    excelHoverTimer = null
  }
  if (excelHideTimer) window.clearTimeout(excelHideTimer)
  excelHideTimer = window.setTimeout(closeExcelMenu, 300)
}

function cancelExcelMenuHide() {
  if (excelHoverTimer) {
    window.clearTimeout(excelHoverTimer)
    excelHoverTimer = null
  }
  if (excelHideTimer) {
    window.clearTimeout(excelHideTimer)
    excelHideTimer = null
  }
}

// Plain-text bodies: the bubble opens when the selection is made; hovering
// the highlighted range brings it back after a page click dismissed it (the
// window click listener closes the bubble without touching the selection).
function onPlainTextHover(event: MouseEvent) {
  if (excelMenu.open || excelBusy.value) return
  const pre = event.currentTarget as HTMLElement
  const sel = window.getSelection()
  if (!sel || sel.rangeCount === 0 || sel.isCollapsed) return
  const anchorEl = sel.anchorNode instanceof Element ? sel.anchorNode : sel.anchorNode?.parentElement
  if (anchorEl?.closest('pre.in-text') !== pre) return
  const mailId = pre.dataset.mailId ?? ''
  if (!mailId) return
  const rect = sel.getRangeAt(0).getBoundingClientRect()
  if (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom) return
  const text = sel.toString().trim()
  if (!text) return
  positionExcelMenu(rect.left + rect.width / 2, rect.bottom + 8, { kind: 'text', mailId, text })
}

function openTextExcelMenu(
  event: { text?: string; attachmentId?: string; x: number; y: number },
  mailId: string,
) {
  if (!mailId) return
  if (event.attachmentId) {
    positionExcelMenu(event.x, event.y, {
      kind: 'attachment', mailId, attachmentId: event.attachmentId,
    })
    return
  }
  const text = event.text?.trim() ?? ''
  if (!text) return
  positionExcelMenu(event.x, event.y, { kind: 'text', mailId, text })
}

// Window-level, not @mouseup on the <pre>: a bottom-to-top drag is usually
// released above the text it selected — over the subject line, the toolbar —
// and a listener on the element never hears that mouseup. The selection
// itself knows which mail it lives in; where the finger lifted is
// irrelevant.
//
// One tick of patience for each of this handler's problems: the selection is
// only final a beat after mouseup, and the click that follows mouseup runs
// the window-level closeExcelMenu — opening synchronously would be undone
// before the user saw it.
function onPlainTextMouseUp() {
  window.setTimeout(() => {
    const sel = window.getSelection()
    if (!sel || sel.rangeCount === 0 || sel.isCollapsed) return
    const anchorEl = sel.anchorNode instanceof Element ? sel.anchorNode : sel.anchorNode?.parentElement
    const focusEl = sel.focusNode instanceof Element ? sel.focusNode : sel.focusNode?.parentElement
    const pre = (anchorEl?.closest('pre.in-text') ?? focusEl?.closest('pre.in-text')) as HTMLElement | null
    if (!pre) return
    const mailId = pre.dataset.mailId ?? ''
    if (!mailId) return
    // Clamp to the mail body. An overshooting drag has the subject line or a
    // toolbar label in it, and those were never part of the mail.
    const range = sel.getRangeAt(0).cloneRange()
    const bounds = document.createRange()
    bounds.selectNodeContents(pre)
    if (range.compareBoundaryPoints(Range.START_TO_START, bounds) < 0) {
      range.setStart(bounds.startContainer, bounds.startOffset)
    }
    if (range.compareBoundaryPoints(Range.END_TO_END, bounds) > 0) {
      range.setEnd(bounds.endContainer, bounds.endOffset)
    }
    const text = range.toString().trim()
    if (!text) return
    const rect = range.getBoundingClientRect()
    positionExcelMenu(rect.left + rect.width / 2, rect.bottom + 8, { kind: 'text', mailId, text })
  }, 0)
}

function openAttachmentExcelMenu(event: MouseEvent, file: MailFile) {
  if (!openedInbound.value) return
  event.preventDefault()
  positionExcelMenu(event.clientX, event.clientY, {
    kind: 'attachment', mailId: openedInbound.value.id, attachmentId: file.id,
  }, file.stored ? '' : 'emails.attachmentNotStored')
}

function closeExcelMenu() {
  excelMenu.open = false
}
window.addEventListener('click', closeExcelMenu)
window.addEventListener('blur', closeExcelMenu)
// Capture phase, because the reading pane scrolls in an inner container and
// scroll events do not bubble. A fixed-position bubble that stays put while
// its selection scrolls away is pointing at nothing.
window.addEventListener('scroll', closeExcelMenu, true)
window.addEventListener('mouseup', onPlainTextMouseUp)
onUnmounted(() => {
  window.removeEventListener('click', closeExcelMenu)
  window.removeEventListener('blur', closeExcelMenu)
  window.removeEventListener('scroll', closeExcelMenu, true)
  window.removeEventListener('mouseup', onPlainTextMouseUp)
})

async function convertExcelSelection() {
  const source = excelMenu.source
  closeExcelMenu()
  if (!source || excelBusy.value || excelMenu.disabledReason) return
  const cached = excelResultCache.get(excelCacheKey(source))
  if (cached) {
    convertedExcelSource.value = source
    excelResult.value = cached
    excelSheet.value = cached.sheets[0]?.name ?? ''
    excelOpen.value = true
    return
  }
  // A spreadsheet attachment is already a table: read it as-is and skip the
  // model entirely. Other sources — or a local read that failed — fall
  // through to the model path below.
  const directFile = directTableAttachment(source)
  if (directFile && (await openAttachmentDirect(directFile))) return
  if (!excelAvailable.value) {
    if (directFile) ElMessage.error(t('emails.excelFailed'))
    return
  }
  await startExcelConversion(source)
}

function directTableAttachment(source: ExcelSource): MailFile | null {
  if (source.kind !== 'attachment') return null
  const file = openedInbound.value?.attachments?.find((a) => String(a.id) === source.attachmentId)
  if (!file?.downloadUrl || !isDirectTableFile(file.fileName, file.contentType)) return null
  return file
}

// Reads the spreadsheet straight from its signed storage URL — the same
// bytes the download button hands out — and presents the parsed preview.
// Returns false when anything about the read fails, so the caller can fall
// back to the model.
async function openAttachmentDirect(file: MailFile): Promise<boolean> {
  const mailId = openedInbound.value?.id
  if (!mailId || !file.downloadUrl) return false
  const source: ExcelSource = { kind: 'attachment', mailId, attachmentId: file.id }
  excelResult.value = null
  convertedExcelSource.value = source
  excelSheet.value = ''
  excelOpen.value = true
  excelBusy.value = true
  try {
    const response = await fetch(file.downloadUrl)
    if (!response.ok) throw new Error(`attachment fetch failed: ${response.status}`)
    const data = await response.arrayBuffer()
    const parsed = await parseTableFile(file.fileName, data)
    const result: ExcelResult = {
      fileName: file.fileName,
      fileData: bytesToBase64(new Uint8Array(data)),
      sheets: parsed.sheets.map((sheet) => ({
        name: sheet.name,
        summary: '',
        columns: sheet.columns,
        rows: sheet.rows.map((cells) => ({ cells })),
        totalRows: String(sheet.totalRows),
      })),
      model: '',
    }
    excelResultCache.set(excelCacheKey(source), result)
    excelResult.value = result
    excelSheet.value = result.sheets[0]?.name ?? ''
    return true
  } catch {
    excelResult.value = null
    excelOpen.value = false
    return false
  } finally {
    excelBusy.value = false
  }
}

function bytesToBase64(bytes: Uint8Array): string {
  let binary = ''
  for (let i = 0; i < bytes.length; i += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000))
  }
  return btoa(binary)
}

async function startExcelConversion(source: ExcelSource) {
  excelResult.value = null
  convertedExcelSource.value = source
  excelSheet.value = ''
  excelOpen.value = true
  excelBusy.value = true
  try {
    const body = source.kind === 'text'
      ? { selectedText: source.text, locale: locale.value }
      : { attachmentId: source.attachmentId, locale: locale.value }
    const response = await post<{ job: ExcelJob }>(
      `/inbound-mails/${source.mailId}/excel`, body, mailExcelRequest,
    )
    excelJobId.value = response.job.id
    sessionStorage.setItem('mailExcelJobId', response.job.id)
    ElMessage.info(t('emails.excelQueued'))
    scheduleExcelJobPoll(300)
  } catch {
    excelOpen.value = false
    excelBusy.value = false
  }
}

async function regenerateExcel() {
  const source = convertedExcelSource.value
  if (!source || excelBusy.value || !excelAvailable.value) return
  await startExcelConversion(source)
}

function resumeExcelJob() {
  const id = sessionStorage.getItem('mailExcelJobId') ?? ''
  if (!id || excelJobId.value === id) return
  excelJobId.value = id
  excelResult.value = null
  excelBusy.value = true
  excelOpen.value = true
  scheduleExcelJobPoll(0)
}

function scheduleExcelJobPoll(delay = 1500) {
  if (excelPollTimer) window.clearTimeout(excelPollTimer)
  excelPollTimer = window.setTimeout(() => void refreshExcelJob(), delay)
}

async function refreshExcelJob(subject = '') {
  const idFromEvent = subject.startsWith('EXCEL_JOB:') ? subject.slice('EXCEL_JOB:'.length) : ''
  const id = excelJobId.value
  if (!id || (idFromEvent && idFromEvent !== id)) return
  try {
    const response = await get<{ job: ExcelJob }>(`/inbound-excel-jobs/${id}`, undefined, mailExcelRequest)
    // The poll timer and the SSE hint race to fetch the same job; only the
    // first response back may announce the terminal state, the later one
    // finds the id already settled and stays silent.
    if (excelJobId.value !== id) return
    const job = response.job
    if (job.status === 'PENDING' || job.status === 'PROCESSING') {
      scheduleExcelJobPoll()
      return
    }
    excelBusy.value = false
    excelJobId.value = ''
    sessionStorage.removeItem('mailExcelJobId')
    if (job.status === 'FAILED' || !job.result) {
      excelOpen.value = false
      ElMessage.error(job.errorMessage || t('emails.excelFailed'))
      return
    }
    job.result.inquiryTemplateId = job.inquiryTemplateId
    job.result.inquiryTemplateCode = job.inquiryTemplateCode
    job.result.inquiryTemplateVersion = job.inquiryTemplateVersion
    excelResult.value = job.result
    if (convertedExcelSource.value) {
      excelResultCache.set(excelCacheKey(convertedExcelSource.value), job.result)
    }
    excelSheet.value = job.result.sheets[0]?.name ?? ''
    excelOpen.value = true
    ElMessage.success(t('emails.excelReady'))
  } catch {
    // The SSE hint is best effort; keep polling through a brief network or
    // gateway restart. An expired mailbox unlock is handled globally.
    if (!locked.value) scheduleExcelJobPoll(3000)
  }
}

// 先问客户，再转。询盘最后要变成报价和合同，那两步都要一个真客户；这里不
// 问，就会在生成报价那一步才卡住——错得更晚，也更难查。
function openSourcingTransfer() {
  const result = excelResult.value
  const sheet = result?.sheets[0]
  if (!result || !convertedExcelSource.value || !sheet?.rows.length) return
  if (Number(sheet.totalRows) > sheet.rows.length) {
    ElMessage.warning(t('emails.sourcingPreviewIncomplete'))
    return
  }
  sourcingForm.customerId = ''
  sourcingForm.customerName = ''
  // 联系人和邮箱可以从来信直接抄——那是这封信的事实。客户是谁不能抄，
  // 那是判断。
  sourcingForm.contactName = openedInbound.value?.fromName || ''
  sourcingForm.contactEmail = openedInbound.value?.fromEmail || ''
  sourcingOpen.value = true
}

async function createSourcingCaseFromExcel() {
  const result = excelResult.value
  const source = convertedExcelSource.value
  const sheet = result?.sheets[0]
  if (!result || !source || !sheet?.rows.length) return
  if (!sourcingForm.customerId) {
    ElMessage.warning(t('emails.sourcingCustomerRequired'))
    return
  }
  const fieldByColumn: Record<string, string> = {
    '产品': 'product', '材质/标准': 'materialStandard', '牌号/等级': 'grade',
    '厚度': 'thickness', '宽度': 'width', '长度/形式': 'lengthOrForm',
    '表面要求': 'surfaceRequirement', '涂层/镀层': 'coating', '公差': 'tolerance',
    '卷重': 'coilWeight', '卷内径': 'coilId', '包装': 'packaging', '交期': 'delivery',
    '付款条件': 'paymentTerms', '贸易术语': 'incoterm', '港口': 'port',
    '单位': 'quantityUnit', '备注': 'remarks', '数量': 'quantity',
  }
  // LLM 结果每列携带模板字段标识（snake_case），按标识对齐——公司用模板
  // 改过表头也不受影响；本地直读的结果没有标识，退回表头映射。价格列不
  // 属于采购明细事实（由工厂报价产生），custom.* 自定义列进 custom_fields。
  const columnKeys = sheet.columnKeys ?? []
  const lines = sheet.rows.map((row) => {
    const line: Record<string, string> & { customFields?: Record<string, string> } = {}
    sheet.columns.forEach((column, index) => {
      const key = columnKeys[index] ?? fieldByColumn[column]
      if (!key || key === 'unit_price' || key === 'total_price') return
      const cell = row.cells[index] ?? ''
      if (key.startsWith('custom.')) {
        ;(line.customFields ??= {})[key] = cell
      } else {
        line[key] = cell
      }
    })
    return line
  })
  creatingSourcingCase.value = true
  try {
    const response = await post<{ sourcingCase: { id: string; caseNo: string } }>('/sourcing-cases', {
      title: result.fileName.replace(/\.xlsx$/i, ''),
      customerId: sourcingForm.customerId,
      customerName: sourcingForm.customerName,
      contactName: sourcingForm.contactName,
      contactEmail: sourcingForm.contactEmail,
      sourceMailId: source.mailId,
      sourceAttachmentId: source.kind === 'attachment' ? source.attachmentId : '0',
      inquiryTemplateId: result.inquiryTemplateId || '0',
      inquiryTemplateCode: result.inquiryTemplateCode || '',
      inquiryTemplateVersion: result.inquiryTemplateVersion || 0,
      lines,
    })
    ElMessage.success(t('procurementIntakes.autoTransferred', { no: response.sourcingCase.caseNo }))
    sourcingOpen.value = false
    excelOpen.value = false
    router.push(`/procurement/intakes?intake=${response.sourcingCase.id}`)
  } finally {
    creatingSourcingCase.value = false
  }
}

function downloadExcel() {
  const result = excelResult.value
  if (!result) return
  const raw = atob(result.fileData)
  const bytes = new Uint8Array(raw.length)
  for (let i = 0; i < raw.length; i++) bytes[i] = raw.charCodeAt(i)
  const url = URL.createObjectURL(new Blob([bytes], {
    type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
  }))
  const link = document.createElement('a')
  link.href = url
  link.download = result.fileName
  link.click()
  window.setTimeout(() => URL.revokeObjectURL(url), 1000)
}

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
    nextTick(() => listScroller()?.scrollTo({ top: 0 }))
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
/* Two surfaces, not one page. The folder rail keeps the white — it is part of
   the application, the same way the navigation is — and the reading side gets
   its own ground, so the seam between them is a change of colour rather than
   an 18px gap you have to look for.

   align-items: stretch so the two colours run the full height of whichever is
   taller; without it the ground stopped at the bottom of the mail and the page
   went pale again underneath it. */
.mailbox {
  display: flex;
  gap: 18px;
  align-items: stretch;
  min-height: 100%;
  background: var(--el-bg-color);
}
.rail {
  flex: none;
  width: 178px;
  /* Back to its own height, which stretch had just taken away — a sticky
     element as tall as its container has nowhere to stick to. */
  align-self: flex-start;
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
  /* One ground for everything on this side — the list, the four folders that
     are tables, the reading page, and the mail's own frame, which is given
     this same colour. The only white left on it is white that means
     something: a row under the cursor, and whatever card a sender drew.
     The padding keeps the content off the edge; it also narrows the container
     below, which is correct — the list should respond to the width it can
     actually use. */
  background: var(--mail-ground);
  padding: 14px 16px;
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
/* 草稿箱, 已定时, 待处理 and 拒收名单 are tables rather than mail lists, and
   Element Plus paints a table white. Left alone they would put back exactly
   the white slab the mail list just stopped being, and a folder would change
   colour depending on which one you clicked.
   Set through the component's own variables rather than by overriding its
   selectors: the library is telling us where its colours come from, and every
   background it paints — table, row, header cell, hover — reads one of these
   four. Nothing is left to a rule of ours that a version bump could stop
   matching. The hover is the list's hover, so a row under the cursor means
   the same thing in every folder. */
.pane :deep(.el-table) {
  --el-table-bg-color: transparent;
  --el-table-tr-bg-color: transparent;
  --el-table-header-bg-color: transparent;
  --el-table-row-hover-bg-color: var(--mail-row-hover);
  --el-table-border-color: var(--mail-divider);
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
/* 琥珀色而不是红色：这是「看一眼」，不是「出事了」。 */
.reply-mismatch {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 12px;
  padding: 8px 12px;
  border: 1px solid var(--el-color-warning-light-5);
  border-radius: 8px;
  background: var(--el-color-warning-light-9);
  color: var(--el-color-warning-dark-2);
  font-size: 13px;
}
.details-toggle {
  margin-left: 8px;
  padding: 0;
  border: 0;
  background: transparent;
  color: var(--el-color-primary);
  font-size: inherit;
  cursor: pointer;
}
.details-toggle:hover {
  text-decoration: underline;
}
/* A definition list, not a table: these are labelled facts about one mail,
   and the label column should size itself to the longest label rather than
   to a guess. */
.mail-details {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 6px 16px;
  margin: 12px 0 0;
  padding: 12px 14px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  background: var(--el-fill-color-lighter);
  font-size: 13px;
}
.mail-details dt {
  color: var(--el-text-color-secondary);
  white-space: nowrap;
}
.mail-details dd {
  margin: 0;
  overflow-wrap: anywhere;
}
.readback {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 12px;
  font-size: 13px;
}
.readback .rb-label {
  color: var(--el-text-color-secondary);
}
.readback .rb-yes {
  color: var(--el-color-success);
  cursor: help;
}
.readback .rb-no,
.readback .rb-off {
  color: var(--el-text-color-secondary);
  cursor: help;
}
.in-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 14px;
}
/* 汇总条：默认展开，但可以折起来。一条十六轮的往来可能挂着九个文件，
   而有时使用者只是想读信。 */
.thread-files-strip {
  margin: 0 0 12px;
  padding: 10px 12px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  background: var(--el-fill-color-lighter);
  font-size: 13px;
}
.thread-files-strip summary {
  color: var(--el-text-color-secondary);
  cursor: pointer;
}
.strip-rows {
  display: flex;
  flex-direction: column;
  gap: 2px;
  margin-top: 8px;
}
.strip-row {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 6px 8px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: inherit;
  font-size: 13px;
  text-align: left;
  cursor: pointer;
}
.strip-row:hover {
  background: var(--el-fill-color);
}
.strip-who {
  max-width: 160px;
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

.excel-context {
  position: fixed;
  z-index: 4000;
  transform: translateX(-50%);
  min-width: 190px;
  max-width: 320px;
  padding: 5px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 7px;
  background: var(--el-bg-color-overlay);
  box-shadow: var(--el-box-shadow-light);
}
.excel-context button {
  width: 100%;
  padding: 8px 12px;
  border: 0;
  border-radius: 5px;
  background: transparent;
  color: var(--el-text-color-primary);
  text-align: left;
  cursor: pointer;
}
.excel-context button:hover,
.excel-context button:focus-visible {
  background: var(--el-fill-color-light);
  color: var(--el-color-primary);
  outline: none;
}
.excel-context button:disabled {
  color: var(--el-text-color-disabled);
  cursor: not-allowed;
}
.excel-context button:disabled:hover,
.excel-context button:disabled:focus-visible {
  background: transparent;
  color: var(--el-text-color-disabled);
}
.excel-context-reason {
  max-width: 240px;
  margin: 3px 8px 6px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
  line-height: 1.45;
}
.excel-preview {
  min-height: 180px;
}
.excel-model {
  margin-bottom: 8px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.sourcing-hint {
  margin: 0 0 14px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.excel-grid {
  max-height: 58vh;
  overflow: auto;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 5px;
}
.excel-grid table {
  width: max-content;
  min-width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}
.excel-grid th,
.excel-grid td {
  max-width: 360px;
  padding: 7px 10px;
  border-right: 1px solid var(--el-border-color-lighter);
  border-bottom: 1px solid var(--el-border-color-lighter);
  white-space: pre-wrap;
  word-break: break-word;
  text-align: left;
  vertical-align: top;
}
.excel-grid th {
  position: sticky;
  top: 0;
  z-index: 1;
  background: var(--el-fill-color-light);
  font-weight: 600;
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
  /* One function per line. These are inline-flex buttons by default, and
     four of them packing into whatever rows fit 178px read as clutter. */
  display: flex;
  width: fit-content;
  margin: 8px 0 0;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
/* Element Plus gives adjacent buttons a left margin — the very thing that
   staggered these into ragged rows. Stacked rows have no use for it. */
.rail-lock + .rail-lock {
  margin-left: 0;
}
/* The group keeps its distance from the folder list above it. */
.rail-grow + .rail-lock {
  margin-top: 16px;
}
</style>
