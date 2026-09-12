<template>
  <!-- 锁着的时候**左栏照样在**，门开在右边的内容区里。

       从前门是整页的：退出一个箱、或者点一下已经退出的那个箱，整个布局被
       门换掉，左边那排信箱跟着消失——于是人被钉在一扇门前，想切到另一个
       还开着的箱都做不到，只能输密码或者离开这一页。

       锁着时左栏只留还能做的那几件：切信箱、加信箱、全部退出。文件夹和
       写信按钮收起来——它们要的正是这个箱的令牌。 -->
  <div v-if="locked !== null" ref="mailboxEl" class="mailbox">
    <!-- A folder rail, not tabs. The distinction matters: folders say "your
         mail lives in these places", tabs said "here are three reports". -->
    <aside ref="railEl" class="rail" :style="colW.rail ? { width: colW.rail + 'px' } : undefined">
      <el-button
        v-if="canWrite && !locked"
        type="primary"
        class="compose"
        @click="composing = true"
      >
        {{ t('emails.compose') }}
      </el-button>
      <!-- 搜索在左栏，不在列表上方——因为它**不属于任何一个文件夹**。
           摆在列表头上的那些天，它看着像「筛这一列」，而它做的是「翻遍所有
           信箱所有文件夹」，两件事差得远。左栏是这一页里唯一比文件夹更高
           的位置，Foxmail、Gmail、Outlook 都把它放在这儿。

           锁着的时候不出现：搜索要的正是令牌。 -->
      <el-input
        v-if="!locked && !isListFilterView"
        v-model="keyword"
        class="rail-search"
        :placeholder="t('emails.searchAll')"
        clearable
        @keyup.enter="reload"
        @clear="reload"
      >
        <template #prefix>🔍</template>
      </el-input>

      <!-- No colleague picker here. This page is "my mail" and stays that
           way; reading somebody else's is a separate, read-only surface. A
           filter here also let a supervisor requeue or abandon a colleague's
           message, which is the salesperson's call, not theirs. -->

      <!-- 但**我自己的**信箱可以有好几个，而文件夹长在信箱底下——「已发送」
           问的是"从这个地址发出去的"，没有主语它就不是一个完整的问题。
           展开/收起的树，照 Foxmail 那个样子。 -->
      <MailboxTree
        ref="switcher"
        v-model="currentAccount"
        :can-add="canWrite"
        :folder="folder"
        :folders="folders"
        :counts="folderCounts"
        :tokens-version="tokensChanged"
        :locked="locked === true"
        :host-folders="hostFolders"
        :drag-accounts="dragging?.accounts ?? []"
        @select="pickFolder"
        @changed="onMailboxesChanged"
        @added="tokensChanged++"
        @create-folder="createFolder"
        @rename-folder="renameFolder"
        @delete-folder="deleteFolder"
        @drop-mails="onDropMails"
      />

      <span class="rail-grow" />
      <!-- 三个工具排在退出上面，中间隔一条线。

           **顺序是有意的**：从前「退出当前邮箱」和「全部退出」并排贴在一起，
           而正下方就是「邮件模板」——想点模板点成全部退出，想退一个退成全退，
           两种误触都发生过。现在退出只剩一个入口、收进下拉里，第一下点开
           什么都不会发生。 -->
      <el-button
        v-if="auth.can('mail:email:write') && !locked"
        link
        class="rail-lock"
        @click="signaturesOpen = true"
      >
        ✍️ {{ t('menu.signatures') }}
      </el-button>
      <el-button
        v-if="auth.can('mail:email:write') && !locked"
        link
        class="rail-lock"
        @click="templatesOpen = true"
      >
        📋 {{ t('menu.templates') }}
      </el-button>
      <el-button v-if="isAdmin" link class="rail-lock" @click="hostOpen = true">
        ⚙️ {{ t('mailGate.hostSettings') }}
      </el-button>

      <!-- 退出的是邮箱，不是 ERP：令牌在服务端就死了，所以下一个坐到这台
           机器前的人会重新遇到那道门。

           **一个一个退。** 从前只有一个按钮而且是全退——那时令牌一个人只有
           一把，撤了就什么都没了。现在退掉当前这个箱，别的箱照开；走人时
           用「全部退出」，而它还要再确认一次：它把所有箱一起关掉，是这一栏里
           唯一一个点错了要重新输好几次密码的动作。 -->
      <!-- 一个能做的都没有时整块不出现：只绑了一个箱又已经退出了，
           点开一个空菜单比没有这个按钮更让人疑惑。 -->
      <template v-if="!locked || mailboxes.length > 1">
        <div class="rail-sep" />
        <el-dropdown trigger="click" class="rail-signout" @command="onSignOut">
          <el-button link class="rail-lock">
            🔒 {{ t('mailGate.signOutMenu') }}
          </el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item v-if="!locked" command="one">
                {{ t('mailGate.signOut') }}
              </el-dropdown-item>
              <!-- 只有一个箱时不给「全部退出」：它和上面那条做的是同一件事，
                   而两条一样的选项挨在一起正是误触的温床。 -->
              <el-dropdown-item v-if="mailboxes.length > 1" command="all" :divided="!locked">
                {{ t('mailGate.signOutAll') }}
              </el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </template>
    </aside>

    <!-- 分隔条：拖它改文件夹栏的宽度。
         锁着的时候也在——左栏那排信箱锁着照样看得见，宽度也就照样该能调。
         双击回到默认宽度：拖坏了总得有条回去的路，而"再拖回来"是拖不准的。 -->
    <div
      class="col-grip"
      role="separator"
      aria-orientation="vertical"
      :aria-label="t('emails.colGrip.rail')"
      :title="t('emails.colGrip.hint')"
      tabindex="0"
      :class="{ grabbing: gripping === 'rail' }"
      @pointerdown="onGripDown('rail', $event)"
      @pointermove="onGripMove"
      @pointerup="onGripUp"
      @pointercancel="onGripUp"
      @dblclick="resetCol('rail')"
      @keydown="onGripKey('rail', $event)"
    />

    <!-- 门。开在内容区里而不是整页，左栏那排信箱才留得住——见上面那段。
         单独一个 section 而不是塞进下面那个：pane 里已经有一条按文件夹分的
         v-if/v-else 链，插进去会把它拆散。 -->
    <section v-if="locked" class="pane">
      <MailboxGate
        :account-id="currentAccount"
        @unlocked="onUnlocked"
        @host-settings="hostOpen = true"
      />
    </section>

    <section v-else class="pane">
      <!-- The mailbox saying it is not receiving. Without this, a revoked
           authorisation fails every poll in silence while the page goes on
           showing the last successful sync as though it were current. -->
      <!-- 那颗「重新登录邮箱」只在授权码真被拒时出现（后端的 needsReauth）。
           从前只要有错误就显示它：263 隔几分钟掐一次空闲连接，每掐一次员工
           就被劝去重输一遍授权码，而重输从来没修好过任何东西。规则和测试在
           lib/syncBanner。 -->
      <el-alert
        v-if="syncBanner.text"
        :type="syncBanner.offerReauth ? 'error' : 'warning'"
        :closable="false"
        show-icon
        class="sync-error"
      >
        <div class="sync-error-body">
          <span>{{ t('emails.syncBroken', { e: syncBanner.text }) }}</span>
          <el-button v-if="syncBanner.offerReauth" size="small" type="primary" plain @click="reauth">
            {{ t('emails.reauth') }}
          </el-button>
          <span v-else class="sync-retrying">{{ t('emails.syncRetrying') }}</span>
        </div>
      </el-alert>

      <!-- ------------------------------------------------- reading a mail -->
      <!-- A page, not a drawer: the mail's id lives in the URL, so a refresh
           reopens the same mail and the browser's back button returns to the
           list, at the page it was on. -->
      <div class="panes" :class="{ 'has-open': readerOpen }">
      <div class="reader-col">
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
        <!-- 一行，不是两行。
             从前是「名字 + 地址」一行、「收件地址 + 详情」另一行，于是名字长
             一点（"The Google Workspace Team"）就折成两行，整块头部四行高，
             把正文推下去——而这四行里没有一句是读信的人要读的。
             现在全部排在一行上，谁长谁省略号；完整的地址在「详情」里。 -->
        <div class="in-from">
          <span class="avatar" :style="avatarStyle(openedInbound.fromEmail)" aria-hidden="true">
            {{ initialOf(openedInbound.fromName || openedInbound.fromEmail) }}
          </span>
          <span class="in-name strong">{{ openedInbound.fromName || openedInbound.fromEmail }}</span>
          <span class="in-addr sub">&lt;{{ openedInbound.fromEmail }}&gt;</span>
          <span class="in-to sub">{{ t('emails.inboundTo', { to: openedInbound.toEmail }) }}</span>
          <!-- 详情 folds the headers away rather than dropping them: a
               reader is for reading, but "which address did this really
               come from" has to be answerable without leaving the page. -->
          <button class="details-toggle" @click="detailsOpen = !detailsOpen">
            {{ detailsOpen ? t('emails.hideDetails') : t('emails.showDetails') }}
          </button>
          <el-button
            v-if="canCreateCustomerFromSender"
            class="sender-customer-action"
            link
            type="primary"
            @click.stop="openCustomerFromSender"
          >
            {{ t('emails.createCustomerFromSender') }}
          </el-button>
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
        <!-- 这三档都**不挂 tooltip**，和列表那一侧同一个理由：措辞本身已经
             把话说完了。「可能已打开」四个字就是那句提示的意思，「没带追踪」
             也是；再弹一块解释只是让鼠标扫过去时蹦一个气泡。
             完整时间还在：鼠标停在时间上有浏览器自带的 title。 -->
        <div v-if="openedInbound.folder === 'SENT'" class="readback">
          <span class="rb-label">{{ t('reader.openedLabel') }}</span>
          <span v-if="openedInbound.openedAt" class="rb-yes" :title="zonedStamp(openedInbound.openedAt)">
            {{ t('emails.maybeOpened') }} · {{ shortTime(openedInbound.openedAt) }}
          </span>
          <span v-else-if="openedInbound.tracked" class="rb-no">{{ t('emails.noOpenYet') }}</span>
          <span v-else class="rb-off">{{ t('reader.noTracking') }}</span>
        </div>
        <!-- 图标条，照 Foxmail：常用的四件事各一颗图标，其余全收进「⋯」。
             从前这里是六到八颗**带文字**的按钮，阅读区窄一点就换行成两排，
             而第二排的起点和第一排对齐，看着像两组不相干的东西。图标不换行
             ——它们加起来不到 160px，怎么窄都放得下。

             图标没有文字，所以每一颗都挂 tooltip，show-after 0：浏览器自带
             的 title 要等将近一秒，等它出来光标早走了，图标就成了猜谜。 -->
        <div class="in-actions">
          <template v-if="canWrite">
            <el-tooltip :content="t('emails.reply')" placement="bottom" :show-after="0" :hide-after="0">
              <button type="button" class="tb" :aria-label="t('emails.reply')" @click="replyToInbound">↩</button>
            </el-tooltip>
            <!-- 常驻，不按「原信有没有别人」来显示。

                 从前它只在算出来的抄送非空时才出现，看着聪明，实际上把一颗
                 按钮的存在和一条业务规则绑死了：抄送要去掉**本人名下全部
                 信箱**的地址，而一个人绑了两个箱、一封信正好发给这两个箱时，
                 抄送就是空的——于是「明明发给了多个人却没有回复全部」。真实
                 发生过。而且按钮时有时无本身就难用：人记不住它什么时候在。 -->
            <el-tooltip :content="t('emails.replyAll')" placement="bottom" :show-after="0" :hide-after="0">
              <button type="button" class="tb" :aria-label="t('emails.replyAll')" @click="replyAllToInbound">↩↩</button>
            </el-tooltip>
            <el-tooltip :content="t('emails.forward')" placement="bottom" :show-after="0" :hide-after="0">
              <button type="button" class="tb" :aria-label="t('emails.forward')" @click="forwardInbound">↪</button>
            </el-tooltip>
          </template>
          <!-- 删除在最右，和前三颗隔一条线：前三颗是「继续这封信」，它是
               「结束这封信」，误触的代价也不一样。 -->
          <span v-if="deleteAction" class="tb-sep" aria-hidden="true" />
          <el-tooltip
            v-if="deleteAction"
            :content="deleteAction.label"
            placement="bottom"
            :show-after="0"
            :hide-after="0"
          >
            <button
              type="button"
              class="tb tb-danger"
              :aria-label="deleteAction.label"
              @click="deleteAction.run()"
            ><el-icon><Delete /></el-icon></button>
          </el-tooltip>

          <span class="grow" />

          <!-- 「⋯」里是其余全部动作。分组用分隔线，顺序按「和这封信的关系
               有多近」：转发存档 → 标记 → 挪去哪儿 → 带出系统。 -->
          <el-dropdown
            v-if="readerMenuAvailable"
            trigger="click"
            placement="bottom-end"
            @command="onReaderCommand"
          >
            <button type="button" class="tb" :aria-label="t('emails.moreActions')">
              <el-icon><MoreFilled /></el-icon>
            </button>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item
                  v-if="canWrite"
                  command="forwardAttachment"
                  :disabled="!openedInbound.hasRaw"
                >
                  {{ t('emails.forwardAsAttachment') }}
                </el-dropdown-item>
                <el-dropdown-item
                  v-if="isInboundView && folder !== 'junk' && folder !== 'trash'"
                  command="unread"
                  :divided="canWrite"
                >
                  {{ t('emails.markUnread') }}
                </el-dropdown-item>
                <el-dropdown-item v-if="folder === 'junk'" command="notJunk" :divided="canWrite">
                  {{ t('emails.notJunk') }}
                </el-dropdown-item>
                <el-dropdown-item v-if="isInboundView && folder === 'archive'" command="unarchive">
                  {{ t('emails.unarchive') }}
                </el-dropdown-item>
                <el-dropdown-item
                  v-else-if="isInboundView && folder !== 'junk' && folder !== 'trash'"
                  command="archive"
                >
                  {{ t('emails.archive') }}
                </el-dropdown-item>
                <el-dropdown-item v-if="folder === 'trash'" command="restore" :divided="canWrite">
                  {{ t('emails.restore') }}
                </el-dropdown-item>

                <!-- 挪进自建文件夹（Issue #362）。真的 MOVE，同步做：成了才回来。

                     文件夹**平铺**在这里，没有再套一层子菜单：Element Plus 的
                     嵌套下拉不好用，而多一层对使用者也没有好处——一个人手上
                     的自建文件夹通常就那么几个。 -->
                <template v-if="canMoveOpened">
                  <el-dropdown-item disabled divided class="menu-head">
                    {{ t('emails.moveTo') }}
                  </el-dropdown-item>
                  <el-dropdown-item v-if="isCustomFolderKey(folder)" command="move:0">
                    {{ t('emails.moveToInbox') }}
                  </el-dropdown-item>
                  <el-dropdown-item
                    v-for="cf in currentCustomFolders"
                    :key="cf.id"
                    :command="`move:${cf.id}`"
                    :disabled="folder === cf.viewKey || moving"
                  >
                    {{ cf.name }}
                  </el-dropdown-item>
                  <el-dropdown-item v-if="!currentCustomFolders.length" disabled>
                    {{ t('emails.noFoldersYet') }}
                  </el-dropdown-item>
                </template>

                <!-- Taking the exchange out of the system. Two errands behind
                     one intent: print it now for the person standing next to
                     you, or save the file to attach to something. Both go
                     through the same audited endpoint. -->
                <template v-if="canExport && openedInbound.threadKey">
                  <el-dropdown-item command="print" divided :disabled="exporting">
                    {{ t('emails.exportPrint') }}
                  </el-dropdown-item>
                  <el-dropdown-item command="save" :disabled="exporting">
                    {{ t('emails.exportSave') }}
                  </el-dropdown-item>
                </template>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
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
            v-for="it in threadForDisplay"
            :key="it.direction + it.id"
            :data-thread-item="threadItemKey(it)"
            class="thread-item"
            :class="{ out: isOwnMail(it) }"
          >
            <button type="button" class="thread-head" @click="toggleThreadItem(it)">
              <el-tag size="small" :type="isOwnMail(it) ? 'info' : 'success'" effect="plain">
                {{ isOwnMail(it) ? t('emails.threadOut') : t('emails.threadIn') }}
              </el-tag>
              <!-- 谁写的，然后「发给谁」。原来这里是 who 加一个光秃秃的地址，
                   而那个地址在「我发出」的行上是收件人、在「收到」的行上是
                   发件人——同一列两个意思，看的人分不出来。现在两行都读作
                   「某某 发给 某某」。 -->
              <span class="strong">{{ turnSenderLabel(it) || it.who }}</span>
              <span v-if="turnRecipients(it)" class="sub ellipsis">
                {{ t('emails.threadTo', { to: turnRecipients(it) }) }}
              </span>
              <span class="grow" />
              <span class="sub" :title="zonedStamp(it.at)">{{ shortTime(it.at) }}</span>
            </button>
            <div v-show="isThreadOpen(it)" class="thread-body">
              <!-- 每一封自己的详情，和单封阅读页那个「详情」同一套内容。
                   放在展开的正文里而不是标题行上：标题行整行就是展开按钮，
                   按钮里不能再套一个按钮。 -->
              <div class="turn-meta">
                <span class="sub">
                  {{ it.fromName ? it.fromName + ' ' : '' }}&lt;{{ turnSenderEmail(it) }}&gt;
                </span>
                <button class="details-toggle" @click="toggleTurnDetails(it)">
                  {{ isTurnDetailsOpen(it) ? t('emails.hideDetails') : t('emails.showDetails') }}
                </button>
              </div>
              <dl v-if="isTurnDetailsOpen(it)" class="mail-details">
                <template v-for="row in turnDetailRows(it)" :key="row.k">
                  <dt>{{ row.k }}</dt>
                  <dd>{{ row.v }}</dd>
                </template>
              </dl>
              <MailBody
                v-if="it.bodyFormat === 'HTML'"
                :html="it.body"
                @selection-context="openTextExcelMenu($event, it.direction === 'IN' ? it.id : '')"
                @selection-clear="closeExcelMenu"
              />
              <!-- 纯文本也走同一个沙箱 frame。以前它是直接插值渲染的，于是
                   正文里的网址只是一行字——点不动，只能手工选中复制。包成
                   <pre> 交给 MailBody 之后链接是真链接，而且 frame 文档里那句
                   <base target="_blank"> 让它在新标签页打开。

                   不在页面里 v-html：收到的信一律只在沙箱里渲染，这条由
                   scripts/check-mail-sandbox.sh 守着——我第一版就是踩了它。 -->
              <MailBody
                v-else
                :html="plainTextToHtml(it.body)"
                @selection-context="openTextExcelMenu($event, it.direction === 'IN' ? it.id : '')"
                @selection-clear="closeExcelMenu"
              />
              <QuotedHistory v-if="it.quoted" :html="it.quoted" />
              <!-- 这一封自己带的附件。放在正文下面、引用历史之后，和阅读单封
                   时的顺序一致。 -->
              <MailAttachments
                v-if="it.attachments?.length"
                :files="it.attachments"
                :converting="convertingAttachment"
                :mail-id="it.direction === 'IN' ? String(it.id) : ''"
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
          <MailBody
            v-else
            :html="plainTextToHtml(openedInbound.bodyText)"
            @selection-context="openTextExcelMenu($event, openedInbound.id)"
            @selection-clear="closeExcelMenu"
          />
          <QuotedHistory v-if="openedInbound.quotedHtml" :html="openedInbound.quotedHtml" />
        </template>
        <template v-if="openedInbound.attachments?.length">
          <el-divider />
          <div class="att-head">
            <h4 class="side-title">{{ t('emails.attachments') }}</h4>
            <!-- 两个以上才给这颗按钮：只有一个附件时它和旁边的「下载」是
                 同一件事，多一颗只会让人挑。 -->
            <el-button
              v-if="openedInbound.attachments.length > 1"
              size="small"
              plain
              :loading="bundling"
              @click="downloadAllAttachments"
            >
              {{ t('emails.downloadAll', { n: openedInbound.attachments.length }) }}
            </el-button>
          </div>
          <MailAttachments
            :files="openedInbound.attachments"
            :converting="convertingAttachment"
            :mail-id="String(openedInbound.id)"
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

      <!-- ---------------------------------------- 看一封写了一半的信 -->
      <!-- 草稿箱从前只有一张表，点一行直接弹出写信框——右边这一栏在草稿箱里
           是空的，左栏点来点去只有它两副样子。现在它和别的文件夹一样：单击
           在右边看，双击（或按「接着写」）才打开写信框。所有桌面邮件客户端
           都是这个分工。

           **不进 URL**，和收信那边不一样。那边把 mail id 写进地址是为了刷新
           回到同一封、也为了把链接发给同事；草稿是私人的（owner_id 一个人），
           发给同事的链接对方打不开，剩下的只有刷新那半条理由，不值得为它多
           一条要维护的路由状态。 -->
      <template v-else-if="openedDraft">
        <div class="detail-top">
          <el-button link class="back-btn" @click="closeDraft">
            ← {{ t('emails.backToList') }}
          </el-button>
        </div>
        <!-- 图标条，和读信那边同一套（.tb）。草稿只有两件事可做：接着写，
             或者不要了。所以没有「⋯」——一个只装得下零个动作的菜单。 -->
        <div class="in-actions">
          <el-tooltip
            v-if="canWrite"
            :content="t('emails.editDraft')"
            placement="bottom"
            :show-after="0"
            :hide-after="0"
          >
            <button
              type="button"
              class="tb"
              :aria-label="t('emails.editDraft')"
              @click="editOpenedDraft"
            ><el-icon><EditPen /></el-icon></button>
          </el-tooltip>
          <span class="tb-sep" aria-hidden="true" />
          <el-tooltip
            :content="common('delete')"
            placement="bottom"
            :show-after="0"
            :hide-after="0"
          >
            <button
              type="button"
              class="tb tb-danger"
              :aria-label="common('delete')"
              @click="dropOpenedDraft"
            ><el-icon><Delete /></el-icon></button>
          </el-tooltip>
          <span class="grow" />
        </div>
        <div v-loading="draftLoading" class="reader-slot">
          <DraftReader :draft="openedDraft" />
        </div>
      </template>

      <!-- 三栏下右边永远在，没选信时给一句话而不是一片空白——空白
           看着像坏了。 -->
      <div v-if="!readerOpen" class="reader-empty">
        {{ folder === 'drafts' ? t('emails.pickADraft') : t('emails.pickAMail') }}
      </div>
      </div><!-- /reader-col -->

      <div
        ref="listEl"
        class="list-col"
        :style="colW.list ? { flex: `0 0 ${colW.list}px` } : undefined"
      >
      <div class="pane-head">
        <!-- Select-all lives in the toolbar, not in a list header: this list
             has no header row, and the toolbar is where the actions are that
             a selection is for. Indeterminate when only some are ticked,
             which is the state that tells you a click will clear rather than
             extend. -->
        <el-checkbox
          v-if="isListFolder(folder) && selectable.length > 0"
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
          <!-- 已读/未读在已发送里没有意义——自己发出去的信本来就是读过的，而且
               行内的按钮也从来不给已发送这两个，工具条不该比行多出两个按钮。 -->
          <el-button v-if="canBulkRead" size="small" @click="bulkMark({ read: true })">
            {{ t('emails.markRead') }}
          </el-button>
          <el-button v-if="canBulkRead" size="small" @click="bulkMark({ read: false })">
            {{ t('emails.markUnread') }}
          </el-button>
          <el-button v-if="folder === 'junk'" size="small" @click="bulkMark({ notJunk: true })">
            {{ t('emails.notJunk') }}
          </el-button>
          <!-- 一键移动（勾选多封）。整条会话一起挪；同一来源文件夹的一次 MOVE 挪完。 -->
          <el-dropdown
            v-if="canBulkMove"
            size="small"
            trigger="click"
            :disabled="bulkBusy"
            @command="bulkMoveTo"
          >
            <el-button size="small" :loading="bulkBusy">
              {{ t('emails.moveTo') }} <el-icon class="el-icon--right"><ArrowDown /></el-icon>
            </el-button>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item v-if="isCustomFolderKey(folder)" :command="0">
                  {{ t('emails.moveToInbox') }}
                </el-dropdown-item>
                <el-dropdown-item
                  v-for="cf in currentCustomFolders"
                  :key="cf.id"
                  :command="cf.id"
                  :disabled="folder === cf.viewKey"
                >
                  {{ cf.name }}
                </el-dropdown-item>
                <el-dropdown-item v-if="!currentCustomFolders.length && !isCustomFolderKey(folder)" disabled>
                  {{ t('emails.noFoldersYet') }}
                </el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
          <el-button
            v-if="folder === 'inbox' || folder === 'starred' || folder === 'sent'"
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
          <!-- 草稿删了就是删了，不进废纸篓：废纸篓是邮件服务器上的一个
               文件夹，而草稿从来没到过服务器。所以这颗按钮说的是「删除」，
               不是「移到废纸篓」——按钮上的字得是它真做的事。 -->
          <el-button
            v-else-if="folder === 'drafts'"
            size="small"
            type="danger"
            plain
            :loading="bulkBusy"
            @click="bulkDropDrafts"
          >
            {{ common('delete') }}
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
        <!-- 表格类文件夹的选择。勾选框在表头（el-table 自己的选择列），能对它做
             什么在这里——和邮件列表用的是同一条工具条，所以「选中之后去哪找按钮」
             这个问题在十个文件夹里只有一个答案。 -->
        <template v-else-if="tablePicked.length">
          <span class="picked-n">
            {{ folder === 'suppressions'
              ? t('emails.pickedAddrN', { n: tablePicked.length })
              : t('emails.pickedN', { n: tablePicked.length }) }}
          </span>
          <span class="grow" />
          <el-button
            v-if="folder === 'scheduled'"
            size="small"
            type="danger"
            plain
            :loading="bulkBusy"
            @click="bulkCancelScheduled"
          >
            {{ t('emails.cancelSchedule') }}
          </el-button>
          <el-button
            v-if="folder === 'attention'"
            size="small"
            type="danger"
            plain
            :loading="bulkBusy"
            @click="bulkAbandon"
          >
            {{ t('emails.abandon') }}
          </el-button>
          <el-button
            v-if="folder === 'suppressions'"
            size="small"
            type="danger"
            plain
            :loading="bulkBusy"
            @click="bulkUnsuppress"
          >
            {{ t('emails.unsuppress') }}
          </el-button>
          <el-button size="small" link @click="clearTablePick">
            {{ t('emails.clearSelection') }}
          </el-button>
        </template>
        <template v-else>
        <!-- 这里从前有一行大字，写着此刻站在哪个文件夹。**去掉了**：左栏那
             一格已经高亮着，同一件事在一屏上说两遍，而它占的是列表最上面
             一整行——桌面上的邮件客户端没有一个把文件夹名字再写一遍。

             搜索的那行留着，因为它说的不是文件夹：它说「这是搜什么搜出来
             的」，而这件事屏幕上没有别处写着。列表里此刻是所有信箱、所有
             文件夹的命中，不说清楚就会被当成这个文件夹里只有这么几封。 -->
        <h2 v-if="isSearching" class="search-title">
          {{ t('emails.searchResults', { q: keyword.trim() }) }}
        </h2>
        <el-button v-if="isSearching" link @click="clearSearch">
          {{ t('emails.searchClear') }}
        </el-button>
        <!-- 排序：点开才展开。
             从前是一排常驻的开关摆在列表最上面（发件人 主题 日期 大小），
             四个词占掉整整一行，而人一天里改排序的次数是零到一次。菜单里
             「按哪一列」和「哪个方向」分两段明写，不再靠「点第二下翻方向」
             ——菜单一点就关，翻没翻人看不见。 -->
        <el-dropdown
          v-if="sortFields.length"
          trigger="click"
          popper-class="mail-sort-menu"
          @command="onSortCommand"
        >
          <button type="button" class="sort-trigger">
            {{ t('emails.sortBar.current', { name: t(`emails.sortBar.${listSort.by}`) }) }}
            <span class="dir" aria-hidden="true">{{ listSort.dir === 'asc' ? '↑' : '↓' }}</span>
          </button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item
                v-for="f in sortFields"
                :key="f"
                :command="`by:${f}`"
                :class="{ 'sort-on': listSort.by === f }"
              >
                {{ t(`emails.sortBar.${f}`) }}
              </el-dropdown-item>
              <el-dropdown-item
                divided
                command="dir:desc"
                :class="{ 'sort-on': listSort.dir === 'desc' }"
              >
                {{ t('emails.sortBar.desc') }}
              </el-dropdown-item>
              <el-dropdown-item
                command="dir:asc"
                :class="{ 'sort-on': listSort.dir === 'asc' }"
              >
                {{ t('emails.sortBar.asc') }}
              </el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
        <span class="grow" />
        <!-- 待处理和拒收名单的筛选框留在这儿，没跟着搬到左栏。
             左栏那个搜的是**邮件**；这两张表一个是 ERP 自己的投递记录、
             一个是拒收地址名单，都不是邮件，全局搜索到不了它们。同一个框
             在这两处做另一件事，才是真的会让人误解的那种复用。 -->
        <template v-if="isListFilterView">
          <el-input
            v-model="keyword"
            :placeholder="t(`emails.search.${folder}`)"
            clearable
            size="small"
            style="width: 240px"
            @keyup.enter="reload"
            @clear="reload"
          />
          <el-button size="small" @click="reload">{{ common('query') }}</el-button>
        </template>
        <!-- 这里从前有一颗「立即收信」。**去掉了**：信箱本来就在自动收——
             打开这一页时拉一次（syncOnOpen），之后守着 IDLE，服务器一有新信
             就推过来。一颗按钮摆在那儿反而是在说「不点它就收不到」，而那不
             是真的；真正没收到的时候点它也没用，那时该看的是上面那条横幅。 -->
        <!-- Clears the unread marks of this view only — the button sits above
             this list, so it does what this list shows.
             Not in junk or the trash: nobody reads their spam folder to the
             end, and what those two need is a way to be rid of it. -->
        <!-- 只看未读。开着时按钮自己是实心的——筛选是一种「列表现在不完整」
             的状态，而这件事必须从屏幕上看得出来，不能只存在地址栏里。 -->
        <el-button
          v-if="canFilterUnread"
          size="small"
          :type="unreadOnly ? 'primary' : ''"
          @click="toggleUnreadOnly"
        >
          {{ t('emails.unreadOnly') }}
        </el-button>
        <el-button v-if="canMarkAllRead" size="small" :loading="markingAll" @click="markAllRead">
          {{ t('emails.markAllRead') }}
        </el-button>
        <!-- Junk out in one click — into the trash, not oblivion. The mail
             worth finding in a spam folder is the customer enquiry the filter
             got wrong, and that is noticed the next day. -->
        <!-- 搜索时不出现：那两个按钮清的是**整个文件夹**，而此刻 total 数
             的是命中数。顶着「3 封」按下去清掉整个垃圾箱，是这一栏里最不能
             出的那种错。 -->
        <el-button
          v-if="folder === 'junk' && total > 0 && !isSearching"
          size="small"
          type="danger"
          plain
          :loading="emptying"
          @click="emptyJunk"
        >
          {{ t('emails.emptyJunk') }}
        </el-button>
        <el-button
          v-if="folder === 'trash' && total > 0 && !isSearching"
          size="small"
          type="danger"
          plain
          :loading="emptying"
          @click="emptyTrash"
        >
          {{ t('emails.emptyTrash') }}
        </el-button>
        <el-button v-if="folder === 'suppressions' && canSuppress" size="small" @click="openSuppress">
          {{ t('emails.addSuppression') }}
        </el-button>
        </template>
      </div>

      <!-- ------------------------ inbox / starred / archive / junk / trash -->
      <!-- 搜索时也走这一块，站在哪个文件夹都一样：命中来自所有文件夹，
           拿草稿箱那张表去渲染它们没有意义。 -->
      <template v-if="isSearching || isInboundView">
        <!-- Spam is the host's verdict, shown read-only as a safety net: the
             mis-flagged customer inquiry is the one mail worth finding here. -->
        <el-alert
          v-if="folder === 'trash' && !isSearching"
          type="info"
          :closable="false"
          show-icon
          class="junk-note"
        >
          {{ t('emails.trashRetention') }}
        </el-alert>
        <el-alert
          v-if="folder === 'junk' && !isSearching"
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
          :accounts="mailboxLabels"
          :current-account="currentAccount"
          :folder="isSearching ? 'inbox' : folder"
          :loading="loading"
          :highlight="isSearching ? keyword : ''"
          :sort="listSort"
          :current="openedInbound?.id"
          @open="openInbound"
          @activate="openMailWindow"
          @star="toggleStar"
          @dragmails="onDragMails"
          @dragend="dragging = null"
        />
        <!-- 空的时候要说清是**哪一种**空。筛着「只看未读」而一封未读都没有，
             和这个文件夹本来就是空的，在屏幕上长得一模一样——不说清楚，人会
             以为信不见了。所以这一档单独一句话，还带一个出口。 -->
        <el-empty
          v-if="!loading && inbound.length === 0"
          :description="
            isSearching
              ? t('emails.searchEmpty', { q: keyword })
              : unreadOnly
                ? t('emails.noUnread')
                : t(folder === 'inbox' ? 'emails.emptyInbox' : 'emails.emptyFolder')
          "
        >
          <el-button v-if="unreadOnly && !isSearching" @click="toggleUnreadOnly">
            {{ t('emails.showAll') }}
          </el-button>
        </el-empty>
      </template>

      <!-- --------------------------------------------------------- drafts -->
      <!-- 和收件箱同一个列表组件，不再是一张表。
           草稿箱从前是「主题 / 保存时间 / 删除」三列，而左栏其余每一格点进去
           都是三行式的邮件列表——同一个左栏底下两副样子，切换时整片区域重排。
           草稿的三行答的是同样三个问题：写给谁、关于什么、写了个什么开头。 -->
      <template v-else-if="folder === 'drafts'">
      <MailList
        v-model:selected="picked"
        :mails="draftRows"
        folder="drafts"
        :loading="loading"
        :current="openedDraft?.id"
        @open="openDraftPreview"
        @activate="editDraftRow"
      />
      <el-empty v-if="!loading && drafts.length === 0" :description="t('emails.noDrafts')" />
      </template>

      <!-- ------------------------------------------------------ scheduled -->
      <template v-else-if="folder === 'scheduled'">
      <el-alert type="info" :closable="false" show-icon class="hint">
        {{ t('emails.scheduledHint') }}
      </el-alert>
      <el-table
        ref="tableRef"
        :data="scheduled"
        v-loading="loading"
        @selection-change="onTableSelect"
      >
        <el-table-column type="selection" width="44" />
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
          :sort="listSort"
          :current="openedInbound?.id"
          @open="openSentRow"
          @activate="openMailWindow"
          @star="toggleStar"
          @dragmails="onDragMails"
          @dragend="dragging = null"
        />
        <el-empty v-if="!loading && mailboxSent.length === 0" :description="t('emails.emptyFolder')" />
      </template>

      <!-- ------------------------------------------------------ attention -->
      <el-table
        v-else-if="folder === 'attention'"
        ref="tableRef"
        :data="messages"
        v-loading="loading"
        class="clickable"
        @row-click="openMessageRow"
        @selection-change="onTableSelect"
      >
        <!-- 没有放弃权限就没有勾选列：能勾、勾完却一个按钮都没有，比不能勾更难懂。 -->
        <el-table-column v-if="canWrite" type="selection" width="44" />
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
      <el-table
        v-else
        ref="tableRef"
        :data="suppressions"
        v-loading="loading"
        @selection-change="onTableSelect"
      >
        <el-table-column v-if="canSuppress" type="selection" width="44" />
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

      <!-- 往下滚就接着加载，照 Foxmail。
           哨兵：它一露到视口里就去取下一页。**不用滚动事件**——列表列在宽屏
           是自己滚、窄屏是整页滚，一套滚动事件接不住两种容器；而
           IntersectionObserver 算的是「实际可见」，中间那层滚动容器的裁剪
           它自己会算进去。 -->
      <div v-if="canLoadMore" ref="moreEl" class="more-sentinel" aria-hidden="true" />
      <div v-if="moreLine !== 'none'" class="more-line">
        <span v-if="moreLine === 'loading'" class="sub">{{ t('emails.loadingMore') }}</span>
        <template v-else-if="moreLine === 'failed'">
          <span class="sub">{{ t('emails.loadMoreFailed') }}</span>
          <el-button size="small" link type="primary" @click="retryLoadMore">
            {{ t('emails.retryLoadMore') }}
          </el-button>
        </template>
        <span v-else class="sub">{{ t('emails.totalMails', { n: total }) }}</span>
      </div>

      <!-- 待处理和已定时还是翻页：它们是操作清单不是信箱——一屏看完一批、
           处理掉、再翻一批，比无限往下滚更合手；而 el-table 套在无限滚动里
           也不好收场。它们的游标记在内存里（见 tablePageCursors）。 -->
      <div v-if="isPagedTable && total > 0" class="pager keyset">
        <span class="sub">{{ t('emails.totalMails', { n: total }) }}</span>
        <el-button size="small" :disabled="!tablePageCursors.length" @click="prevTablePage">
          {{ t('emails.prevPage') }}
        </el-button>
        <el-button size="small" :disabled="!nextCursor" @click="nextTablePage">
          {{ t('emails.nextPage') }}
        </el-button>
      </div>
      </div><!-- /list-col -->

      <!-- 列表和阅读区之间的分隔条。模板里阅读区写在列表前面（见 .panes 那段
           注释），靠 order 排位置，所以这一条虽然写在最后，画出来是在中间。
           窄屏下两栏是上下叠着的，那时它自己藏起来——横着拖一条竖线，在一个
           没有并排的布局上说不通。 -->
      <div
        class="col-grip for-list"
        role="separator"
        aria-orientation="vertical"
        :aria-label="t('emails.colGrip.list')"
        :title="t('emails.colGrip.hint')"
        tabindex="0"
        :class="{ grabbing: gripping === 'list' }"
        @pointerdown="onGripDown('list', $event)"
        @pointermove="onGripMove"
        @pointerup="onGripUp"
        @pointercancel="onGripUp"
        @dblclick="resetCol('list')"
        @keydown="onGripKey('list', $event)"
      />
      </div><!-- /panes -->
    </section>

    <EmailComposer
      ref="composer"
      v-model="composing"
      :mailboxes="composableMailboxes"
      :current-account="composeAccount"
      @sent="onSent"
      @saved="onDraftSaved"
    />

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

  <CustomerFromMailDialog v-model:open="customerCreateOpen" :draft="customerCreateDraft" />

  <el-dialog
    v-model="excelTemplateOpen"
    :title="t('emails.selectExcelTemplate')"
    width="min(520px, 92vw)"
    append-to-body
  >
    <el-form label-position="top" v-loading="excelTemplatesBusy">
      <el-form-item :label="t('emails.excelTemplate')" required>
        <el-select
          v-model="selectedInquiryTemplateId"
          :placeholder="t('emails.excelTemplatePlaceholder')"
          style="width: 100%"
        >
          <el-option
            v-for="template in excelTemplates"
            :key="template.id"
            :value="template.id"
            :label="`${template.name} (v${template.version})`"
          >
            <span>{{ template.name }} (v{{ template.version }})</span>
            <el-tag v-if="template.isDefault" size="small" type="success" effect="plain" style="margin-left: 8px">
              {{ t('emails.defaultTemplate') }}
            </el-tag>
          </el-option>
        </el-select>
      </el-form-item>
      <p v-if="selectedExcelTemplate?.description" class="sub">{{ selectedExcelTemplate.description }}</p>
    </el-form>
    <template #footer>
      <el-button @click="excelTemplateOpen = false">{{ common('cancel') }}</el-button>
      <el-button
        type="primary"
        :disabled="!selectedInquiryTemplateId || excelTemplatesBusy"
        @click="confirmExcelTemplate"
      >
        {{ t('emails.generateExcel') }}
      </el-button>
    </template>
  </el-dialog>

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
          <span v-if="excelResult.model && selectedExcelTemplate"> · {{ t('emails.excelTemplateUsed', { name: selectedExcelTemplate.name, version: selectedExcelTemplate.version }) }}</span>
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
        v-if="excelResult && auth.can('sales:inquiry:write')"
        @click="openSourcingTransfer"
      >
        {{ t('emails.createSourcingCase') }}
      </el-button>
      <el-button v-if="excelResult && excelAvailable" :loading="excelBusy" @click="regenerateExcel">
        {{ t('emails.switchTemplateAndRegenerate') }}
      </el-button>
      <el-button v-if="excelResult" type="primary" @click="downloadExcel">
        {{ t('emails.downloadExcel') }}
      </el-button>
    </template>
  </el-dialog>

  <!-- 客户和联系人都来自基础数据。联系人按客户联动，邮箱只显示主数据快照。 -->
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
          :placeholder="t('emails.sourcingCustomerPlaceholder')"
          @selected="selectSourcingCustomer"
        />
      </el-form-item>
      <el-form-item :label="t('emails.sourcingContact')" required>
        <el-select
          v-model="sourcingForm.contactId"
          filterable
          :loading="sourcingContactsLoading"
          :disabled="!sourcingForm.customerId"
          :placeholder="sourcingForm.customerId ? t('emails.sourcingContactPlaceholder') : t('emails.sourcingSelectCustomerFirst')"
          style="width:100%"
        >
          <el-option
            v-for="contact in sourcingContacts"
            :key="contact.id"
            :value="contact.id"
            :label="sourcingContactLabel(contact)"
            :disabled="!contact.email"
          />
        </el-select>
        <div v-if="sourcingForm.customerId && !sourcingContactsLoading && !sourcingContacts.length" class="sourcing-contact-help">
          {{ t('emails.sourcingNoActiveContacts') }}
        </div>
      </el-form-item>
      <el-form-item :label="t('emails.sourcingContactEmail')">
        <el-input
          :model-value="selectedSourcingContact?.email || ''"
          readonly
          :placeholder="t('emails.sourcingContactEmailAuto')"
        />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="sourcingOpen = false">{{ t('emails.close') }}</el-button>
      <el-button
        type="primary"
        :loading="creatingSourcingCase"
        :disabled="!sourcingForm.customerId || !sourcingForm.contactId"
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
import {
  del,
  download,
  get,
  http,
  mailExcelRequest,
  mailHostRequest,
  post,
  quietErrors,
  saveBlob,
  put,
} from '../api'
import { shortTime, zonedStamp } from '../lib/zonedtime'
import { humanSize } from '../lib/humanSize'
import { needsConversion } from '../lib/attachmentPreview'
import { folderNameProblem, isCustomFolderKey, viewForFolderKey, type CustomFolder } from '../lib/mailFolders'
import { turnRecipients, turnSenderEmail, turnSenderLabel } from '../lib/threadTurn'
import { attachmentHintKey } from '../lib/attachmentHint'
import { plainTextToHtml } from '../lib/linkifyText'
import { replyAllRecipients } from '../lib/replyAll'
import { syncBanner as buildSyncBanner, type SyncBanner } from '../lib/syncBanner'
import {
  DEFAULT_SORT,
  parseSort,
  sortFieldsFor,
  sortFor,
  sortFromCommand,
  sortParam,
  type MailSort,
  type SortField,
} from '../lib/mailSort'
import { isDirectTableFile, parseTableFile } from '../lib/attachmentExcel'
import { mailDetailRows, replyToDiffers } from '../lib/mailDetails'
import { SEP_LIST, SEP_RAIL, clampCol, clearWidth, readWidth, writeWidth, type Col } from '../lib/paneWidths'
import {
  isListFolder,
  pickedRows as pickedRowsOf,
  selectAllState,
  selectableRows,
  toggleAll,
} from '../lib/mailSelection'
import type { InquiryTemplate } from '../lib/inquiryTemplates'
import { customerDraftFromMail } from '../lib/mailCustomerDraft'
import { onLive } from '../live'
import { useAuthStore } from '../stores/auth'
import EmailComposer from '../components/EmailComposer.vue'
import MailReader, { type Mail } from '../components/MailReader.vue'
import MailAttachments, { type MailFile } from '../components/MailAttachments.vue'
import MailboxGate from '../components/MailboxGate.vue'
import MailboxTree from '../components/MailboxTree.vue'
import {
  adoptVerification,
  allTokens,
  initialMailbox,
  clearAll,
  forgetMailbox,
  searchScopeHeader,
  unlockedMailboxes,
  useMailbox,
  type VerifyResponse,
  settleMailbox,
} from '../lib/mailUnlock'
import type { DropTarget } from '../lib/dragMails'
import MailHostDialog from '../components/MailHostDialog.vue'
import MailSignatureDialog from '../components/MailSignatureDialog.vue'
import MailTemplatesDialog from '../components/MailTemplatesDialog.vue'
import MailList, { type MailRow } from '../components/MailList.vue'
import DraftReader, { type DraftDetail } from '../components/DraftReader.vue'
import { draftPreviewContext, draftToRow } from '../lib/draftRow'
import { shouldReloadList } from '../lib/liveInbox'
import { moreStatus, shouldLoadMore, type MoreState } from '../lib/infiniteList'
import CustomerFromMailDialog from '../components/CustomerFromMailDialog.vue'
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
  ArrowDown,
  MoreFilled,
} from '@element-plus/icons-vue'
// Shared mail-surface tokens. Global rather than scoped: the list is its own
// component, and the two have to agree on density or it reads as accidental.
import '../styles/mailbox.css'

interface InboundMail {
  id: string
  fromEmail: string
  fromName: string
  toEmail: string
  toName?: string
  subject: string
  snippet: string
  threadKey: string
  isRead: boolean
  isStarred: boolean
  hasAttachments: boolean
  // 这封信答过没有（#364）。只有列表给，单封读取不给——那一页上「回复」
  // 按钮就在手边，不需要再说一遍。
  isAnswered?: boolean
  // Whether the original MIME is still archived. Only set on the detail read;
  // absent in list rows, which is why 作为附件转发 lives on the open mail.
  hasRaw?: boolean
  // Set only on search results: which folder the hit was found in.
  matchFolder?: string
  // 同上，跨信箱那一维：这条命中是哪个箱的。列表行上用来挂信箱标签。
  matchAccount?: number
  // 这封信落在哪个信箱。**只在单封读取时有值**，服务端给的。
  // 写信框的发件人读它（见 composeAccount）：站在 A 箱里点开的可能是 B 箱
  // 收到的信，回信得从 B 发出去。
  accountId?: number | string
  // Which mailbox folder this copy sits in. 'SENT' is what tells the reader
  // to show 对方是否已读 — once both are an InboundMail, nothing else does.
  folder?: string
  // 已发送这一侧才有：'ERP' 是我们自己的投递记录（对方邮箱没留下副本），
  // 'HOST' 是邮箱自己的已发送里那一封。两者点开去的是不同的详情。
  kind?: string
  // 对方是否已读, for a Sent copy the ERP has a delivery record for. Three
  // states between them: openedAt set, tracked without openedAt, neither.
  openedAt?: string
  tracked?: boolean
  // The details panel.
  rawSize?: number
  // 真正的回信地址，以及收信服务器验过的两个身份。
  replyTo?: string
  cc?: string
  // 整段 To 头（给人看的）和拆好的人（给「回复全部」用）。老信还没补时
  // toAll 是第一个收件人，toParties 只有一个人。
  toAll?: string
  toParties?: { name?: string; email: string }[]
  ccParties?: { name?: string; email: string }[]
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
    previewKind?: string
  }[]
}

// 已发送文件夹里的一封，形状和收件箱那边完全一样（后端也确实返回同一个
// InboundMail），区别只在 kind：'ERP' 是投递记录，'HOST' 是邮箱里的真信。
type SentMail = InboundMail

// 「需要处理」那一列：ERP 自己发出去的那条投递记录，以及它为什么卡住。
//
// 名字不叫 Message，是因为 Message 在这个文件里已经是 Element Plus 的一个
// 图标组件了。两个同名的东西一个是值一个是类型，读的人分不清，编译器也
// 分不清——它一直把这里当成那个图标。
interface AttentionMessage {
  id: string
  campaignId: string
  kind: string
  senderId: string
  senderName: string
  toEmail: string
  toName: string
  customerName: string
  subject: string
  // QUEUED / SENDING / ACCEPTED / DELIVERED / SEND_UNKNOWN / SOFT_BOUNCED /
  // HARD_BOUNCED / COMPLAINED / FAILED / NEEDS_ATTENTION
  status: string
  attemptCount: number
  lastError: string
  // 为什么要人来看这一封，用能直接照做的话写的。
  attentionReason: string
  queuedAt: string
  sentAt: string
  deliveredAt: string
  openedAt: string
}

// 草稿箱里的一封。收件人/抄送/密送和附件都随草稿存着，所以重新打开恢复的
// 是整封信，不只是那几行字。
interface Draft {
  id: string
  subject: string
  body: string
  bodyFormat: string
  signatureId: string
  kind: string
  recipients?: { email: string; name: string }[]
  cc?: { email: string; name: string }[]
  bcc?: { email: string; name: string }[]
  attachments?: { id: string; fileName: string; contentType: string; fileSize: string }[]
  updatedAt: string
  recipientCount: number
  // 只有列表接口给这两样：正文头一句，和带没带附件。草稿箱是三行式的邮件
  // 列表了，第三行显示的就是 snippet——而为了一行摘要把每封草稿的正文整篇
  // 搬过来是不合算的，所以服务端先剥成文字再截短。打开一封时给的是 body。
  snippet?: string
  hasAttachments?: boolean
  sendMode: string
  replyToInboundId: string
  forwardInboundId: string
  forwardAsAttachment: boolean
  disableTracking: boolean
}

// 定时发送队列里的一批。按批不按封：一次群发排一条。
interface Scheduled {
  campaignId: string
  campaignNo: string
  subject: string
  // 还有多少个收件人在等。
  pendingCount: number
  toNames: string
  scheduledAt: string
  sendMode: string
  bodyFormat: string
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
const canCreateCustomer = computed(() => auth.can('masterdata:customer:write'))

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
const isInboundView = computed(() => folder.value in INBOUND_VIEWS || isCustomFolderKey(folder.value))
/** 发给列表接口的 view：固定文件夹走映射，自建的原样传。规则在 lib/mailFolders。 */
const currentView = computed(() => viewForFolderKey(folder.value, INBOUND_VIEWS))
// Every mailbox folder pages by cursor. A page number is meaningless on a
// list that grows at the top, and 已发送 grows at the top like the rest.
const isKeysetView = computed(
  () => isInboundView.value || folder.value === 'sent' || folder.value === 'attention' || folder.value === 'scheduled',
)
// Junk and the trash get a delete-everything button instead: marking a spam
// folder read is housekeeping nobody wants, and in the trash it is meaningless.
// 搜索时也不出现，和「清空垃圾箱」同一个理由，只是后果更隐蔽：那两个按钮
// 至少会弹一句确认，这一个按下去就把**整个文件夹**标成已读了——而屏幕上
// 摆着的是几封跨文件夹、跨信箱的命中。按钮自己的注释写着「它做的是这个列表
// 显示的事」，搜索一开这句话就不成立了。
const canMarkAllRead = computed(
  () => isInboundView.value && !isSearching.value
    && folder.value !== 'junk' && folder.value !== 'trash',
)
const keyword = ref('')

// Two characters, matching the server's floor. One character matches most of
// the mailbox, which is not a result set — it is the mailbox with extra steps.
// Counted in characters rather than bytes, because one Chinese character is a
// word's worth of meaning.
//
// **几乎不再看站在哪个文件夹。** 从前这里加着 isInboundView，因为搜索框长
// 在列表头上，一个文件夹一个框；框挪到左栏之后它就是这一页唯一的搜索，站在
// 草稿箱里打字也该搜——搜的本来就不是"这个文件夹"。
//
// 例外是下面那两张不是邮件的表。
const isSearching = computed(
  () => !isListFilterView.value && [...keyword.value.trim()].length >= 2,
)

// 这两张表不是邮件：待处理是 ERP 自己的投递记录，拒收名单是一串地址。
// 全局搜索翻的是收到的信，到不了它们，所以它们各自留着自己的筛选框。
const isListFilterView = computed(
  () => folder.value === 'attention' || folder.value === 'suppressions',
)

// 退出搜索：清掉关键词，回到刚才那个文件夹。
//
// 单独一个按钮，因为清空输入框那个小叉在左栏里，而人的眼睛此刻在右边的
// 结果上。
function clearSearch() {
  keyword.value = ''
  reload()
}

// 信箱号 → 地址。搜索结果横跨信箱，每一行得说自己是哪个箱的。
const mailboxLabels = computed(() => {
  const out: Record<number, string> = {}
  for (const b of mailboxes.value) out[b.id] = b.email
  return out
})

// 写信框默认从哪个箱发。
//
// **打开着一封信的时候，跟着那封信走**，不跟着左栏的高亮走。搜索横跨信箱，
// 站在 A 箱里点开的可能是 B 箱收到的信——跟着高亮走就是「读 B 的信、从 A
// 回过去」，客户看到的发件人和他寄到的地址对不上，而且没有任何提示。
//
// 没开着信（点「写邮件」）时才是左栏那个箱：那时没有别的信息可依据，而人
// 正站在那个箱上。
//
// 这封信是从哪个箱来的由服务端说（单封读取带回 accountId），不由前端记——
// 刷新一下、或者别人把带 ?mail= 的链接发过来，前端手里什么都没有。
// 要求那个箱**还开着**（composableMailboxes 是发件人下拉的那份名单）：
// 退出过的箱不在下拉里，指过去的话下拉会是空白一格，而人只会看到「发件人
// 没填」却不知道为什么。退回左栏那个箱，至少是个能选中的选项。
const composeAccount = computed(() => {
  const own = Number(openedInbound.value?.accountId ?? 0)
  if (own && composableMailboxes.value.some((b) => b.id === own)) return own
  return currentAccount.value
})
// 列表按哪一列排。地址栏说了算（applyRoute 写它），这里只是镜像。
const sort = ref<MailSort>(DEFAULT_SORT)
const sortSide = computed(() => (folder.value === 'sent' ? 'sent' : 'inbox'))
const listSort = computed(() => sortFor(sortSide.value, sort.value))
// 排序栏给哪几列。**搜索时一列都不给**：搜索走的是另一条查询，那条只按
// 时间倒序回，请求里根本没有排序参数——排序栏摆在那儿，点了什么都不会变，
// 而箭头还会翻个面，看着像"排了但排错了"。
//
// 已发送从前是个例外（那条查询搜索和排序可以同时用），而搜索框搬到左栏
// 之后，站在已发送里打字走的也是全局搜索那条路了——例外跟着消失，这里
// 不再单开一条 return。
const sortFields = computed<SortField[]>(() => {
  if (isSearching.value) return []
  if (folder.value === 'sent') return sortFieldsFor('sent')
  if (!isInboundView.value || keyword.value.trim()) return []
  return sortFieldsFor('inbox')
})
// 排序菜单点了一项：`by:size`、`dir:asc`。规则（换一列用那一列的自然方向、
// 方向是菜单里明写的两项）在 lib/mailSort 里，那儿测得到。
function onSortCommand(cmd: string) {
  pushState({ sort: sortParam(sortFromCommand(listSort.value, cmd)) })
}

// 只看未读（issue #368 那条「支持查看未读邮件」）。
//
// 做成筛选，不做成「未读排前面」。排序那条路在这里是坏的：列表按未读优先排
// 的话，点开一封信、它一变成已读就立刻往下跳——光标底下的行不见了，而
// 「标已读不重排」是这个列表刻意守着的一条规矩（见 markRow 那一带的注释）。
// 筛选没有这个问题：标已读仍然只改那一行的样子，它要到下一次重新拉列表时
// 才消失，而那时人已经看完了。
//
// 开关的状态进地址栏（unread=1），刷新和后退都保得住。
const unreadOnly = ref(false)
// 搜索时不给这个开关：搜索走的是另一条查询，服务端在那条路上不接这个筛选，
// 排序栏消失也是同一条理由。
const canFilterUnread = computed(() => isInboundView.value && !isSearching.value)
function toggleUnreadOnly() {
  pushState({ unread: !unreadOnly.value })
}
// ------------------------------------------------ 往下滚就接着加载 ---
// 邮件列表（收件箱那一族、搜索结果、已发送）往下接；待处理和已定时是表格，
// 还是翻页，理由见模板里那两段注释。
const isMailList = computed(() => isSearching.value || isInboundView.value || folder.value === 'sent')
const isPagedTable = computed(() => folder.value === 'attention' || folder.value === 'scheduled')

const moreState = computed<MoreState>(() => ({
  hasMore: !!nextCursor.value,
  loading: loading.value,
  loadingMore: loadingMore.value,
  failed: moreFailed.value,
  supported: isMailList.value,
}))
// 哨兵只在还有下一页时挂出来：到底之后留着它，观察器会一直盯着一个永远
// 可见的元素。
const canLoadMore = computed(() => moreState.value.supported && !!nextCursor.value)
const moreLine = computed(() => moreStatus(moreState.value, currentRows.value > 0))
const currentRows = computed(() =>
  isSearching.value || isInboundView.value ? inbound.value.length : mailboxSent.value.length,
)

function loadMore() {
  if (!shouldLoadMore(moreState.value)) return
  void load({ append: true, quiet: true })
}

// 人点「重试」。**单独一个入口**，因为 shouldLoadMore 里那条「上一次失败了
// 就别再取」挡的是观察器（否则对着一个坏掉的接口每秒敲一次），不是挡人。
// 把这两种意图混在一个函数里的后果是那颗按钮点下去什么都不发生。
function retryLoadMore() {
  moreFailed.value = false
  loadMore()
}

const moreEl = ref<HTMLElement | null>(null)
let moreObserver: IntersectionObserver | null = null
// rootMargin：提前 300px 就开始取，滚到底的时候下一页多半已经在了。
// root 不指定（用视口）：列表列在宽屏是自己滚、窄屏是整页滚，而
// IntersectionObserver 算可见性时会把中间那层滚动容器的裁剪算进去，
// 两种布局用同一套代码。
watch(moreEl, (el) => {
  moreObserver?.disconnect()
  moreObserver = null
  if (!el) return
  moreObserver = new IntersectionObserver(
    (entries) => {
      if (entries.some((e) => e.isIntersecting)) loadMore()
    },
    { rootMargin: '300px 0px' },
  )
  moreObserver.observe(el)
})
onUnmounted(() => moreObserver?.disconnect())

// ------------------------------------------------ 表格类的翻页 ---
// 游标记在内存里，不进地址栏（见 pushState 上的注释）。一页一个游标，
// 「上一页」就是把上一个弹出来重放。
const tablePageCursors = ref<string[]>([])
function nextTablePage() {
  if (!nextCursor.value) return
  tablePageCursors.value = [...tablePageCursors.value, tableCursor.value]
  tableCursor.value = nextCursor.value
  load()
}
function prevTablePage() {
  const stack = tablePageCursors.value
  if (!stack.length) return
  tableCursor.value = stack[stack.length - 1]
  tablePageCursors.value = stack.slice(0, -1)
  load()
}
const tableCursor = ref('')

const page = ref(1)
const pageSize = 20
const total = ref(0)
const loading = ref(false)
const messages = ref<AttentionMessage[]>([])
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
// 往下滚接下一页的三个状态。**只有这三个**：在取、上一次失败了、已经接过
// 至少一页。别的（还有没有下一页）由 nextCursor 本身回答。
const loadingMore = ref(false)
const moreFailed = ref(false)
// 已经接出来几页（1 = 只有第一页）。
//
// 记页数而不是记一个「接过没有」的布尔，是因为**重新拉的时候要把这几页拼
// 回来**。屏幕上任何一个「刷新一下列表」的动作——处理完一封信返回、批量
// 归档、拖走几封、全部已读——都会走重新拉；如果那一下只拿回第一页，滚了
// 三页的人会突然发现列表只剩二十五行，而他刚才读的那封在第七十行。翻页
// 时代这件事不明显（重新拉的是**当前那一页**，长度不变），无限滚动把它
// 放大成每次操作都回到顶上。
const loadedPages = ref(1)
const markingAll = ref(false)
const emptying = ref(false)
// What the server last said went wrong with this mailbox, empty when healthy.
const syncBanner = ref<SyncBanner>({ text: '', offerReauth: false })
// 当前在看哪个信箱。0 = 全部（还没绑过，或者只有一个）。
const currentAccount = ref(0)
// 这个人名下的信箱清单。退出一个之后要知道还剩哪些，好切过去。
const mailboxes = ref<{ id: number; email: string; isDefault: boolean }[]>([])
// 发件人下拉只列**还开着**的箱。退出了 163 之后它不该还在里面——留着的话
// 「一个一个退出」只退了一半：读不到 163 的信，却还能以 163 的地址给客户
// 写信，而那正是退出想停掉的事。
//
// 令牌存在 localStorage 里，Vue 看不见它变。tokensChanged 是那一下的信号：
// 解锁、退出、全部退出都拨一次。
const tokensChanged = ref(0)
const composableMailboxes = computed(() => {
  void tokensChanged.value
  const open = new Set(unlockedMailboxes())
  return mailboxes.value.filter((b) => open.has(b.id))
})
const switcher = ref<{ reload: () => Promise<void> } | null>(null)
// **我的全部地址**，不是一个。一封信的发件人是其中任何一个，它就是"我发出"
// 的——哪怕它是从收件箱里进来的（发给自己的信）。
//
// 从前这里是单个 accountEmail。一个人绑了两个箱之后，从另一个箱发出去的信
// 会被认成"对方"发的，标签打反，而且不报任何错。
const myAddresses = ref<Set<string>>(new Set())

function isOwnMail(it: { direction: string; counterparty: string }) {
  return it.direction === 'OUT'
    || myAddresses.value.has(it.counterparty.trim().toLowerCase())
}

// 信箱清单变了（切换器加载完、新绑了一个、换了默认）。
function onMailboxesChanged(boxes: { id: number; email: string; isDefault: boolean }[]) {
  mailboxes.value = boxes
  myAddresses.value = new Set(boxes.map((b) => b.email.trim().toLowerCase()).filter(Boolean))
  const next = settleMailbox(currentAccount.value, boxes)
  if (!next || next === currentAccount.value) return
  const wasUnset = !currentAccount.value
  // 还没选过就落在默认那个上——服务端按「默认排最前」返回，所以取第一个。
  // 选了一个**不是自己的**箱也落回默认箱：令牌里记的箱号来自服务端，改版前
  // 的旧令牌现在会被读成 1 号箱，不落回去的话人会卡在一个空视图上。
  currentAccount.value = next
  // 从一个不是自己的箱落回来，下面那个 watch（before 有值）会换令牌、重新
  // 导航、拉列表，这里不用再拉。
  if (!wasUnset) return
  // **0 → id 这一跳必须跟着重新拉一次列表。** 信箱清单是异步来的，而列表在
  // 它之前就已经带着 accountId=0 发出去了——那一次拉的是"全部信箱合并"。
  // 左侧此刻高亮着默认箱，右边列着两个箱的信，两者对不上，而且**不会自己
  // 纠正**：下面那个 watch 要求 before 有值才动，这一跳被它跳过了。
  //
  // 锁着的时候别去拉列表：那些接口全要解锁令牌，拉出来的只有一串 403 弹窗
  // 盖在门上。左栏现在锁着也在（门开在内容区里），所以这条路会在锁着时走到。
  if (locked.value !== false) return
  load()
  refreshUnread()
}
// The mail being read full-page. Set from the URL, never directly: opening a
// mail is a navigation, so refresh reopens it and back returns to the list.
const openedInbound = ref<InboundMail | null>(null)
const customerCreateDraft = computed(() => openedInbound.value
  ? customerDraftFromMail(openedInbound.value, myAddresses.value)
  : null)
const canCreateCustomerFromSender = computed(() => {
  return canCreateCustomer.value && customerCreateDraft.value !== null
})

// Folded by default, and folded again on every open: the details are for the
// one mail somebody is questioning, not a preference to carry into the next.
const detailsOpen = ref(false)
watch(openedInbound, () => {
  detailsOpen.value = false
})

// 回信地址与发件人不一致。判断在 lib/mailDetails（那儿写着为什么值得提醒，
// 也测得到）；这里只是把它接到当前打开的那封信上。
const replyToMismatch = computed(() => replyToDiffers(openedInbound.value))

// 工具条最右那颗垃圾桶到底做哪件事。
//
// 回收站里它是**彻底删除**（那里的「删除」只能是这个意思，再挪一次没地方
// 可挪），别处是挪进回收站。两件事共用一颗按钮但绝不能共用一个措辞——
// tooltip 说的就是按下去会发生什么，因为图标本身分不出这两者。
//
// 已发送里那封是邮箱服务器上的正本，删得掉；ERP 自己的投递记录（kind=ERP）
// 没有正本可删，那时不给这颗按钮。
const deleteAction = computed(() => {
  const m = openedInbound.value
  if (!m || !isInboundView.value) return null
  if (m.kind === 'ERP') return null
  if (folder.value === 'trash') {
    return { label: t('emails.purge'), run: purgeOpened }
  }
  return { label: t('emails.toTrash'), run: () => markOpened({ deleted: true }) }
})

// 「移动到」那一组给不给。和从前那颗按钮同一个条件。
const canMoveOpened = computed(
  () => isInboundView.value
    && folder.value !== 'trash'
    && folder.value !== 'junk'
    && openedInbound.value?.folder !== 'SENT',
)

// 「⋯」里到底有没有东西。一个点开是空的菜单比没有这颗按钮更糟。
//
// 改成图标条时这条守卫一度掉了：站在已发送里、又没有写信和导出权限的人，
// 那颗「⋯」点开是一片空白。条件是下面菜单里每一项的条件求或——多一项就
// 要在这里也添一笔，这是这种守卫的代价，但比一个空菜单便宜。
const readerMenuAvailable = computed(() => {
  const m = openedInbound.value
  if (!m) return false
  const inbound = isInboundView.value
  return (
    canWrite.value // 作为附件转发
    || (inbound && folder.value !== 'junk' && folder.value !== 'trash') // 标为未读/归档
    || folder.value === 'junk' // 不是垃圾邮件
    || folder.value === 'trash' // 还原
    || canMoveOpened.value // 移动到
    || (canExport.value && !!m.threadKey) // 导出
  )
})

// 「⋯」菜单只有一个出口，省得每一项各写一个 @click——它们本来就是一组
// 「对这封信做点什么」，一个 command 串把它们摊在一处，加一项也只改一处。
function onReaderCommand(cmd: string) {
  if (cmd.startsWith('move:')) {
    void moveOpenedTo(Number(cmd.slice(5)))
    return
  }
  switch (cmd) {
    case 'forwardAttachment': return void forwardInboundAsAttachment()
    case 'unread': return void markOpened({ read: false })
    case 'notJunk': return void markOpened({ notJunk: true })
    case 'archive': return void markOpened({ archived: true })
    case 'unarchive': return void markOpened({ archived: false })
    case 'restore': return void markOpened({ deleted: false })
    case 'print': return void printThread()
    case 'save': return void saveThread()
  }
}

// 这几行怎么来的在 lib/mailDetails：双击弹出的那个单独窗口画的是同一份，
// 两边各写一遍的话，哪天多一行「密送」就只会加在其中一处。
const detailRows = computed(() => mailDetailRows(openedInbound.value, t))

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
  // 按哪一列排：`size:desc` 这种，见 lib/mailSort。空 = 日期倒序。
  // 放进地址栏是为了刷新和后退都保得住——排到一半刷新一下回到按日期排，
  // 人会以为自己看错了。
  sort: string
  // 只看未读。同样进地址栏，同样的理由：筛着一半刷新一下变回全部，人会
  // 以为收件箱多出来一批信。
  unread: boolean
  // 在看哪个信箱。空 = 还没选（第一次进来，切换器还没加载完）。
  //
  // 放进 URL 而不是只留在内存里：刷新会回到默认箱而人以为自己还在另一个箱
  // 里；后退更糟——它会退回一个属于**上一个箱**的游标，然后拿它去翻当前
  // 这个箱，翻出来的东西没有任何报错但也没有任何意义。
  acct: string
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
    folder: FOLDER_KEYS.has(f) || isCustomFolderKey(f) ? f : 'inbox',
    page: Number.isInteger(p) && p > 1 ? p : 1,
    q: one(q.q),
    sent: one(q.sent) === 'mailbox' ? 'mailbox' : 'erp',
    mail: /^\d+$/.test(one(q.mail)) ? one(q.mail) : '',
    msg: /^\d+$/.test(one(q.msg)) ? one(q.msg) : '',
    acct: /^\d+$/.test(one(q.acct)) ? one(q.acct) : '',
    sort: sortParam(parseSort(one(q.sort))),
    unread: one(q.unread) === '1',
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
  if (s.acct) query.acct = s.acct
  if (s.sort) query.sort = s.sort
  if (s.unread) query.unread = '1'
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

// ------------------------------------------------ 三栏宽度可以拖 ---
// 文件夹栏 | 列表 | 阅读区，两条分隔条，各拖各的。存在 localStorage，下次
// 打开还是这个宽度。为什么要能拖、为什么没拖过就一个数都不存，见
// lib/paneWidths。
//
// 宽度写成行内样式，不是 CSS 里的类：它是某一个人拖出来的数，没有第二个
// 地方知道它。没拖过时行内样式整个不写（值是 null），CSS 里那两个默认值
// ——文件夹栏 208px、列表 clamp(280px, 34%, 400px)——继续管事。
const railEl = ref<HTMLElement | null>(null)
const listEl = ref<HTMLElement | null>(null)
// 想要多宽（colWish）和此刻真用多宽（colW）分开记。
//
// 窗口一变窄，装不下的那一栏得让位，否则阅读区被挤到只剩一个词宽。但让位
// 的只是"此刻用的"那个数：人想要的宽度不变，窗口再拉大，宽度自己回来。
// 两个数合成一个的话，缩一次窗口就把人拖出来的设置永久改小了。
const colWish = reactive<Record<Col, number | null>>({
  rail: readWidth('rail', localStorage),
  list: readWidth('list', localStorage),
})
const colW = reactive<Record<Col, number | null>>({ ...colWish })
// 正在拖哪一条。只为了让那条分隔条在拖的过程中亮着——手早就离开了它原来的
// 位置（指针被捕获了），没有这点反馈就不知道自己还按着。
const gripping = ref<Col | null>(null)
// 按下去那一刻的位置和宽度。之后每一次移动都从这两个数算起，不累加增量：
// 累加会把每一次的取整误差叠起来，拖一趟下来鼠标和分隔条差出好几像素。
let grip: { col: Col; x0: number; w0: number } | null = null

function colEl(col: Col): HTMLElement | null {
  return col === 'rail' ? railEl.value : listEl.value
}

// 这一栏最宽能到哪儿：它所在的那块地方，减掉旁边那些不归它的东西。
//
// 列表量的是自己的父节点——「列表 + 分隔条 + 阅读区」那一块——再减掉分隔条，
// 所以拖到头时阅读区正好是 0。父节点而不是「信箱区减掉文件夹栏」：后者漏掉
// 了它们之间那条分隔条。
//
// 文件夹栏减掉两条分隔条，再减掉列表**此刻**的宽度：拖它的时候列表不跟着变
// 窄（它有自己的宽度），能让出来的只有阅读区。
function roomFor(col: Col): number {
  if (col === 'list') {
    const panes = listEl.value?.parentElement?.clientWidth ?? 0
    return panes ? panes - SEP_LIST : Infinity
  }
  const box = mailboxEl.value?.getBoundingClientRect().width || 0
  if (!box) return Infinity
  const listNow = listEl.value?.getBoundingClientRect().width ?? 0
  return box - SEP_RAIL - listNow - SEP_LIST
}

// 拖到了这个宽度：想要的和正用的一起改，然后存。拖的时候人看得见边界在哪，
// 所以这里两个数是同一个。
function dragTo(col: Col, px: number) {
  const w = clampCol(px, roomFor(col))
  colW[col] = w
  colWish[col] = w
}

function saveCol(col: Col) {
  const w = colWish[col]
  if (w !== null) writeWidth(col, w, localStorage)
}

function onGripDown(col: Col, ev: PointerEvent) {
  const el = colEl(col)
  if (!el) return
  grip = { col, x0: ev.clientX, w0: el.getBoundingClientRect().width }
  gripping.value = col
  // 指针捕获：拖快了鼠标会跑到分隔条外面，甚至跑出窗口。捕获之后移动和松开
  // 都还发到这一条上，不会拖到一半"手滑脱了"。
  ;(ev.currentTarget as HTMLElement).setPointerCapture(ev.pointerId)
  // 顺手把旁边的文字选上是这类控件最常见的那个脏东西：拖一条分隔条，半列
  // 邮件主题跟着变蓝。
  ev.preventDefault()
}

function onGripMove(ev: PointerEvent) {
  if (!grip) return
  dragTo(grip.col, grip.w0 + (ev.clientX - grip.x0))
}

function onGripUp() {
  if (!grip) return
  const col = grip.col
  grip = null
  gripping.value = null
  saveCol(col)
}

// 双击：忘掉这个数，回到默认宽度。拖坏了总得有条回去的路，而"再拖回来"
// 是拖不准的。
function resetCol(col: Col) {
  colW[col] = null
  colWish[col] = null
  clearWidth(col, localStorage)
}

// 键盘也能调。一条只能用鼠标拖的分隔条，对用键盘的人等于不存在——而它管的
// 是"主题能看多长"，不是装饰。Home 是回默认，对应鼠标那边的双击。
function onGripKey(col: Col, ev: KeyboardEvent) {
  if (ev.key === 'Home') {
    ev.preventDefault()
    resetCol(col)
    return
  }
  const step = ev.shiftKey ? 48 : 16
  const dx = ev.key === 'ArrowLeft' ? -step : ev.key === 'ArrowRight' ? step : 0
  if (!dx) return
  const el = colEl(col)
  if (!el) return
  ev.preventDefault()
  dragTo(col, el.getBoundingClientRect().width + dx)
  saveCol(col)
}

// 窗口变了之后重新收一遍：装不下的让位，装得下的回到人想要的宽度。存下来
// 的数一个都不动。
function refitCols() {
  for (const col of ['rail', 'list'] as Col[]) {
    const wish = colWish[col]
    colW[col] = wish === null ? null : clampCol(wish, roomFor(col))
  }
}
window.addEventListener('resize', refitCols)
onUnmounted(() => window.removeEventListener('resize', refitCols))
// 什么时候收：这两块出现的那一刻，不在 onMounted——那时它们还没画出来，
// 量到的宽度是 0。
//
// **两块都要盯**，这是个真会咬人的地方：信箱区（mailboxEl）等锁的状态问
// 出来之后就一直在，锁上也在（左栏那排信箱锁着照样看得见）；而三栏所在的
// 那块（listEl 的父节点）只有解锁之后才画。令牌过夜就没了，第二天进来是
// 「先锁着、输密码、再出现三栏」——只盯信箱区的话，那一次收发生在三栏还
// 不存在的时候，量不到宽度，于是上次在大屏上拖出来的宽度原样套到小屏上，
// 阅读区被挤成一条。
watch([mailboxEl, listEl], () => refitCols())

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

// 游标不再进地址栏：邮件列表改成往下滚就接着加载之后，「翻到哪一页」不再是
// 一个状态——只有「已经接了几页」，而那和滚动位置一样，是屏幕的样子不是内容
// 的样子。一条带游标的链接发给同事，对方打开会落在列表中间、上面什么都没有。
//
// 表格类（待处理、已定时）还在翻页，但它们的游标也不进地址栏了：翻到第三页
// 刷新一下回到第一页，比一条发出去打不开的链接好接受。
function pushState(over: Partial<UrlState>) {
  const cur = applied ?? parseQuery(route.query)
  const next = { ...cur, ...over }
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
  router.push({ query: toQuery(next) })
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
  folder.value = s.folder
  page.value = s.page
  keyword.value = s.q
  sort.value = parseSort(s.sort)
  unreadOnly.value = s.unread
  // 换了文件夹/关键词/排序/筛选，接过的那几页和表格的翻页位置都作废：它们
  // 记的是「在上一份名单里走到哪」。不清的话，换个文件夹第一次往下滚会拿
  // 上一份名单的游标去要下一页。
  if (
    !prev ||
    prev.folder !== s.folder ||
    prev.q !== s.q ||
    prev.sort !== s.sort ||
    prev.unread !== s.unread
  ) {
    loadedPages.value = 1
    moreFailed.value = false
    tableCursor.value = ''
    tablePageCursors.value = []
  }  // 地址栏说了在看哪个箱就照做。这是后退/前进/刷新走的那条路：
  // 不同步的话，URL 里写着 A 箱而列表按 B 箱拉。
  if (s.acct) currentAccount.value = Number(s.acct)
  if (
    !prev ||
    prev.folder !== s.folder ||
    prev.page !== s.page ||
    prev.q !== s.q ||
    prev.sent !== s.sent ||
    prev.acct !== s.acct ||
    prev.sort !== s.sort ||
    // 切「只看未读」也要重拉。#429 把这一条漏了：那时它靠的是「改筛选会清
    // 游标、游标变了就重拉」，而第一页的游标本来就是空的——于是在第一页上
    // 点这颗按钮，地址栏变了、按钮亮了，列表一动不动。游标移出地址栏之后
    // 那条间接的路彻底没了，这里必须直说。
    prev.unread !== s.unread
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

// 右边那一栏此刻有没有内容。三种来源（收到的信、发出的信、写了一半的信）
// 都摊在这一个判断上，因为「有东西看」和「给一句空状态」必须是同一个问题的
// 正反面——分开写就会有第三种情况：两边都以为对方在管，于是空状态压在
// 一封信上面。
const readerOpen = computed(() => !!openedInbound.value || outboundOpen.value || !!openedDraft.value)

const requeueOpen = ref(false)
const requeueRow = ref<AttentionMessage | null>(null)
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
        // 带上刚绑好的那个地址。不带的话，服务端验的是**默认信箱**——
        // 一个 263 的人刚用 Google 绑了 Gmail，默认箱还是那个 263 的、
        // 密码绑的，于是这一步必然报「请输入邮箱密码或授权码」：
        // 绑成功了，门却打不开，两句话都是真的而人看不懂。
        const resp = await http.post(
          '/mailbox/verify',
          { secret: '', email: boundEmail },
          mailHostRequest,
        )
        const acct = adoptVerification(resp.data.data as VerifyResponse)
        tokensChanged.value++
        // 刚绑的那个箱直接切过去：人刚在 Google 上挑完账号，想看的就是它。
        if (acct) currentAccount.value = acct
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
    // accountId 是**这把令牌开的那个箱**，也就是数据实际来自哪个箱。
    //
    // 一定要用它来定初始高亮，不能只靠「默认箱」：从菜单点进 邮箱 时地址栏
    // 里没有 acct，currentAccount 会落到默认箱 A，而请求带的是上次留下的
    // B 的令牌——网关只认令牌，于是左边高亮 A、右边列的是 B 的信。
    //
    // 更糟的是接着回信：写信框的发件人跟着 currentAccount 走，于是「读 B
    // 收到的信、从 A 发出去」——正是按箱发信要消掉的那件事。
    const d = await get<{ unlocked: boolean; accountId?: number | string }>(
      '/mailbox/lock-status',
    )
    locked.value = !d.unlocked
    // 0 = 一个箱都没绑，那时交给 onMailboxesChanged 落到默认箱；令牌里记的箱
  // 不是自己的（改版前的旧令牌现在会被读成 1 号箱）也在那里落回默认箱。
    // 地址栏里的 acct 优先级更高，随后由 applyRoute 覆盖。
    currentAccount.value = initialMailbox({ token: Number(d.accountId ?? 0) })
  } catch {
    locked.value = true
  }
  if (locked.value === false) init()
})

function onUnlocked() {
  locked.value = false
  // 门里刚收下一批令牌（adoptVerification），发件人下拉要跟着更新。
  tokensChanged.value++
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

// 退出**当前这一个**信箱。别的箱照开。
//
// 从前是全退，因为令牌一个人只有一把。现在一个箱一把，撤掉当前这把之后
// 手上还有别的箱的——所以退完不是弹登录门，而是切到剩下的任意一个箱。
// 一把都不剩了才是真的退出，那时门才该出来。
async function lockMailbox() {
  const leaving = currentAccount.value
  try {
    await post('/mailbox/lock')
  } finally {
    // 不管服务端那一下成没成，本地都当它退了：撤销失败却把屏幕开着，
    // 而人已经以为关掉走开了，是这两者里更糟的那个。
    forgetMailbox(leaving)
    tokensChanged.value++
    switcher.value?.reload()
    const rest = mailboxes.value.filter((b) => b.id !== leaving)
    if (!rest.length) {
      locked.value = true
      return
    }
    ElMessage.success(t('mailGate.signedOutOne'))
    // 切到剩下的第一个，**开着没开着都切**——决定交给上面那个 watch：
    // 手上有令牌就进去，没有就把门摆出来。
    //
    // 从前这里是「有令牌才切，否则原地弹门」，那会把门指向**刚退掉的那个
    // 箱**——人刚点了退出，屏幕立刻问他要那个箱的密码，看着像是没退成功。
    currentAccount.value = rest[0].id
  }
}

// 全部退出：手上每一把都报给服务端撤掉。共用电脑走人时用的那一个。
async function lockAllMailboxes() {
  try {
    await post('/mailbox/lock-all', { tokens: allTokens() })
  } finally {
    clearAll()
    tokensChanged.value++
    locked.value = true
  }
}

function init() {
  applied = null
  applyRoute()
  checkSyncHealth()
  loadExcelCapability()
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
//
// 角标永远刷；列表要不要重拉，规则在 lib/liveInbox。这里从前写着「开着一封
// 信就不重拉」——那是两栏布局的规矩（读信会盖住列表），三栏之后列表就在打开
// 的信旁边，而一封信几乎总是开着的，于是列表**永远**不更新：角标在涨、列表
// 纹丝不动、非得手动刷新。两边各自的道理见那个文件。
//
// 重拉是安静的（不转圈）：这不是人点出来的动作，一转圈就像是页面自己出了
// 什么事。新的一行出现在顶上就是全部的反馈。
onUnmounted(
  onLive((e) => {
    if (e.type === 'mail.excel_job.changed') {
      void refreshExcelJob(e.subject)
      return
    }
    if (e.type !== 'mail.inbound') return
    refreshUnread()
    const reload = shouldReloadList(e.subject, {
      folder: folder.value,
      searching: isSearching.value,
      // 接过下一页的人正在往回翻历史，重拉会把列表换成头二十五行——他滚了
      // 半天的东西就没了。角标照刷。
      onFirstPage: loadedPages.value <= 1,
      picked: picked.value.length,
      dragging: !!dragging.value,
      bulkBusy: bulkBusy.value,
      currentAccount: currentAccount.value,
    })
    if (reload) void load({ quiet: true })
  }),
)

// The rail badge has to be current whichever folder is open, so it has its
// own cheap fetch rather than riding on the inbox list load.
//
// 顺带刷一次左侧那排信箱角标。它们和顶上这个数字一样会过期，而且过期得
// **更难看出来**：顶上那个说的是眼前这个箱，看着列表就知道对不对；角标说
// 的是另一个箱，除非切过去，否则没有任何东西能拆穿它。
//
// 每次收到新信都会走到这里，所以两个数字总是一起更新的——挂在同一个函数
// 上而不是各自找时机，正是为了不出现「一个新了一个没新」。
async function refreshUnread() {
  void switcher.value?.reload()
  try {
    const d = await get<{ unreadCount: number }>('/inbound-mails', {
      page: 1,
      page_size: 1,
      // 同上：算哪个箱的未读由令牌决定。
    })
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

async function openDraft(row: { id: string }) {
  composing.value = true
  // Wait for the dialog to mount before handing it the draft, or the watch
  // that resets a fresh compose would wipe what we just loaded.
  await nextTick()
  await composer.value?.openDraft(row.id)
}

function onDraftSaved() {
  loadDraftCount()
  if (folder.value === 'drafts') load()
  // 右边正看着的那一封可能就是刚存的这封，重新取一遍，否则预览停在存之前
  // 的样子——而人刚刚改的正是它。
  if (openedDraft.value) showDraft(openedDraft.value.id)
}

async function dropDraft(row: { id: string; subject?: string }) {
  await ElMessageBox.confirm(
    t('emails.dropDraftHint', { s: row.subject || t('emails.noSubject') }),
    common('delete'),
    { type: 'warning' },
  )
  await del(`/email-drafts/${row.id}`)
  ElMessage.success(t('emails.draftDropped'))
  if (openedDraft.value?.id === row.id) closeDraft()
  load()
  loadDraftCount()
}

// ------------------------------------------------- 草稿箱的列表和预览 ---
// 草稿箱现在也是「左边一列、右边一封」。转换成列表行的规则在 lib/draftRow：
// 全是「这一样没有时说什么」的判断，而草稿可以三样都没有。
const draftRows = computed(() =>
  drafts.value.map((d) =>
    draftToRow(d, {
      noRecipient: t('emails.draftNoRecipient'),
      andMore: (first: string, n: number) => t('emails.draftAndMore', { first, n }),
    }),
  ),
)

// 右边正在看的那一封。列表接口只给摘要，正文和抄送要单独取一趟——和收件箱
// 一样的分工，理由也一样：为了一行摘要把两百封的正文整篇搬过来不合算。
const openedDraft = ref<DraftDetail | null>(null)
const draftLoading = ref(false)

async function showDraft(id: string) {
  draftLoading.value = true
  try {
    const d = await get<{ draft: DraftDetail }>(`/email-drafts/${id}`)
    openedDraft.value = d.draft ?? null
  } catch {
    // 取不着就退回列表，而不是让右边停在上一封上——右边显示着 A 而列表选中
    // 的是 B，比右边空着更容易让人对错了内容。
    openedDraft.value = null
  } finally {
    draftLoading.value = false
  }
}

function openDraftPreview(row: { id: string }) {
  showDraft(row.id)
}

function closeDraft() {
  openedDraft.value = null
}

// 双击列表里的一行 = 接着写。
function editDraftRow(row: { id: string }) {
  if (canWrite.value) openDraft(row)
}

function editOpenedDraft() {
  if (openedDraft.value) openDraft(openedDraft.value)
}

async function dropOpenedDraft() {
  if (openedDraft.value) await dropDraft(openedDraft.value)
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
  pushState({ folder: key, page: 1, q: '', mail: '', sort: '' })
}

// 左栏点的是「某个信箱的某个文件夹」——一下回答了两件事。
//
// 同一个箱里换文件夹走上面那条；换箱则要**先换令牌再导航**，那件事归
// currentAccount 那个 watch 管，所以这里只是把想去的文件夹交给它。
// 两条各自 pushState 的话会连着导航两次，中间那一次拉的是「新箱 + 旧文件夹」，
// 白花一趟请求，还在历史里留下一个谁都没到过的位置。
// ---------------------------------------------------------------- 自建文件夹

// 每个信箱在服务器上的全部文件夹，带角色。左栏按同一级别画；「移动到」只列
// 角色为 CUSTOM 的。
const hostFolders = ref<Record<number, CustomFolder[]>>({})
const moving = ref(false)
const currentCustomFolders = computed(() => (hostFolders.value[currentAccount.value] ?? []).filter((f) => f.role === 'CUSTOM'))

/** 拉一个信箱的文件夹清单。失败就当没有：左栏少一截，比弹一句错强。 */
async function loadCustomFolders(accountId: number) {
  if (!accountId) return
  try {
    const d = await get<{ folders?: CustomFolder[] }>('/mail-folders', { account_id: accountId }, quietErrors)
    hostFolders.value = { ...hostFolders.value, [accountId]: (d.folders ?? []).map((f) => ({
      id: Number(f.id), accountId: Number(f.accountId), name: f.name, viewKey: f.viewKey, role: f.role,
    })) }
  } catch {
    // 锁着、或者服务器暂时连不上：留着上一次的
  }
}

async function askFolderName(title: string, initial = ''): Promise<string | null> {
  try {
    const { value } = await ElMessageBox.prompt(t('mailGate.newFolderAsk'), title, {
      inputValue: initial,
      inputValidator: (v: string) => {
        const p = folderNameProblem(v)
        return p ? t(`mailGate.folderName.${p}`) : true
      },
    })
    return value.trim()
  } catch {
    return null
  }
}

async function createFolder(accountId: number) {
  const name = await askFolderName(t('mailGate.newFolder'))
  if (!name) return
  try {
    await post('/mail-folders', { accountId: String(accountId), name })
    await loadCustomFolders(accountId)
  } catch {
    // 拦截器已经弹了后端的原因（重名、服务器拒绝）
  }
}

async function renameFolder(cf: CustomFolder) {
  const name = await askFolderName(t('mailGate.renameFolder'), cf.name)
  if (!name || name === cf.name) return
  try {
    await put(`/mail-folders/${cf.id}`, { name })
    await loadCustomFolders(cf.accountId)
    // 正停在这个文件夹里：它的 key 变了，跟过去
    if (folder.value === cf.viewKey) switchFolder(`F:${name}`)
  } catch {
    // 同上
  }
}

async function deleteFolder(cf: CustomFolder) {
  try {
    await ElMessageBox.confirm(t('mailGate.deleteFolderAsk', { name: cf.name }), t('mailGate.deleteFolder'), { type: 'warning' })
  } catch {
    return
  }
  try {
    await del(`/mail-folders/${cf.id}`)
    await loadCustomFolders(cf.accountId)
    if (folder.value === cf.viewKey) switchFolder('inbox')
  } catch {
    // 里面还有信之类的原因，后端说了
  }
}

/** 把打开的这封信挪进某个文件夹（0 = 收件箱）。成了就回到列表。 */
async function moveOpenedTo(folderId: number) {
  const id = openedInbound.value?.id
  if (!id || moving.value) return
  moving.value = true
  try {
    await post(`/inbound-mails/${id}/move`, { folderId: String(folderId) })
    ElMessage.success(t('emails.moved'))
    pushState({ mail: '' })
    load()
  } catch {
    // 后端的原因拦截器已经弹了
  } finally {
    moving.value = false
  }
}

watch(currentAccount, (id) => { void loadCustomFolders(id) }, { immediate: true })

let pendingFolder: string | null = null
function pickFolder(accountId: number, key: string) {
  // accountId = 0 是不跟信箱走的那两个（待处理、拒收名单）。
  if (accountId && accountId !== currentAccount.value) {
    pendingFolder = key
    currentAccount.value = accountId
    return
  }
  switchFolder(key)
}

// 左栏那几个数字。**只有当前这个箱有**：别的箱服务端只给了未读总数，
// 分文件夹的计数没有，编一个出来比空着坏得多。
const folderCounts = computed<Record<string, number>>(() => ({
  inbox: unreadCount.value,
  drafts: drafts.value.length,
  attention: attentionCount.value,
}))

// 退出那个下拉。**全部退出还要再确认一次**：它把所有箱一起关掉，是这一栏里
// 唯一一个点错了要重新输好几次密码的动作。
async function onSignOut(cmd: string) {
  if (cmd === 'one') {
    await lockMailbox()
    return
  }
  await ElMessageBox.confirm(
    t('mailGate.signOutAllAsk', { n: mailboxes.value.length }),
    t('mailGate.signOutAll'),
    { type: 'warning' },
  )
  await lockAllMailboxes()
}

function reload() {
  pushState({ page: 1, q: keyword.value, mail: '' })
}

// 切信箱 = 重新开始翻这个箱。
//
// 游标必须清掉：它编的是**上一个箱**的排序位置，带着它翻新箱会从一个毫无
// 意义的地方开始，而且不会报错——只是列表看起来少了一截。
//
// 也写进地址栏。仓库的习惯是可分享的状态放 URL，而这里还有一层：不写的话
// 刷新会回到默认箱，而人以为自己还在另一个箱里；浏览器后退更糟——它会退回
// 一个属于**上一个箱**的游标，然后拿它去翻当前这个箱。
watch(currentAccount, (now, before) => {
  if (!before || now === before) return
  // 换一把令牌。**必须在发请求之前**——令牌决定服务端给你看哪个箱
  // （见网关 requireMailUnlock），带着旧箱那把去拉新箱的列表，拿回来的
  // 还是旧箱的信。
  //
  // 没有这个箱的令牌 = 刚把它退出过。那时门要重新出来，只针对这个箱。
  if (!useMailbox(now)) {
    pendingFolder = null
    locked.value = true
    return
  }
  // **切回一个还开着的箱，门要收起来。**
  //
  // 漏掉这一句的后果和门占整页是同一个毛病的另一半：点了一个退出过的箱，
  // 门出来；再点回一个开着的箱，令牌换好了、列表也能拉了，而门还杵在那儿。
  //
  // 必须在 pushState 之前：applyRoute 开头有 `if (locked !== false) return`，
  // 还锁着的话这次导航会被它整个吞掉，于是列表停在上一个箱。
  const wasLocked = locked.value
  locked.value = false
  keyword.value = ''
  // 左栏点的是「哪个箱的哪个文件夹」，两件事一次导航说完。没点文件夹时
  // （解绑后自动切、新绑一个箱）留在原来那个文件夹，和从前一样。
  const goto = pendingFolder
  pendingFolder = null
  // 换箱等于换了一份完全不同的名单。
  loadedPages.value = 1
  moreFailed.value = false
  tableCursor.value = ''
  tablePageCursors.value = []
  pushState({ page: 1, q: '', mail: '', acct: String(now), sort: '', ...(goto ? { folder: goto } : {}) })
  // 从锁着的状态回来时，页面上那些只在解锁后才拉的东西（草稿数、待处理数、
  // 同步健康）都还是空的或者过期的。init 会把它们一起补上。
  if (wasLocked) {
    init()
    return
  }
  refreshUnread()
  // 横幅说的是「当前这个箱」，所以切换时立刻重问一次。它自己是一分钟轮询
  // 一次的，不问的话最长有一分钟红条在替**上一个箱**说话——而人刚切过来，
  // 只会以为是这个箱坏了。
  checkSyncHealth()
})

function backToList() {
  pushState({ mail: '' })
}

// 接上去还是换掉。抽成一个函数，好让五种列表（收件箱、搜索、已发送、已定时、
// 待处理）不会有一种漏掉 append。
function concatRows<T>(append: boolean, cur: T[], next: T[]): T[] {
  return append ? [...cur, ...next] : next
}

// quiet：不转圈。给后台自己发起的重拉用（新信到了），人点出来的都要转。
// append：接在现有这批后面，而不是换掉——往下滚到底时走这条。
//
// 两者共用一个函数而不是各写一份，是因为「从哪儿取、取回来怎么解」这两件事
// 两边一模一样，分开写迟早会有一边漏掉某个参数（筛选、排序、信箱）。
// 每一次取列表都领一个号。**只有最新的那一号能把结果写进列表。**
//
// 没有这道闸的后果有两种，都不会报错：
//
//   一、接下一页的请求还在飞，人点了别的文件夹。新文件夹的第一页先回来，
//       旧文件夹的第二页后回来——而它手里攥着 append=true，于是收件箱的信
//       被接在了星标列表底下，游标也被换成收件箱的。
//   二、同一个文件夹里：接下一页在飞，人把上面几封批量删了。删完那一下重新
//       拉第一页，这时第一页已经够到原来「第二页」的位置；那个还在飞的请求
//       回来一接，同一封信出现两遍（MailList 按 id 做 key，会直接撞 key）。
//
// 领号而不是 AbortController：请求本身取消不取消无所谓，要紧的是别把过期的
// 结果写进去。
let loadSeq = 0

// 回 true 表示这一次的结果真的写进列表了；被更新的一次挤掉、或者失败了，
// 回 false。reloadPages 靠它决定还要不要往下拼。
async function load(opts: { quiet?: boolean; append?: boolean; single?: boolean } = {}): Promise<boolean> {
  const append = opts.append === true
  // 重新拉的时候把已经接出来的那几页拼回来，而不是只拿第一页。single 是
  // reloadPages 自己往下调时用的，防止无限套娃。
  if (!append && !opts.single && loadedPages.value > 1) {
    return reloadPages(opts)
  }
  const seq = ++loadSeq
  const fresh = () => seq === loadSeq
  // 接下一页时用服务端上一次给的游标；重新拉时从头开始。表格类还在翻页，
  // 它们的位置记在 tableCursor 里。
  const cursor = append ? nextCursor.value : isPagedTable.value ? tableCursor.value : ''
  if (append) {
    loadingMore.value = true
    moreFailed.value = false
  } else {
    loadedPages.value = 1
    moreFailed.value = false
    if (!opts.quiet) loading.value = true
  }
  try {
    if (isSearching.value) {
      // A search crosses folders, so it is a different request with a
      // different shape — not the folder list with a parameter. Gmail works
      // the same way, and for the same reason: somebody who remembers a
      // phrase does not remember where they filed it.
      //
      // 它也横跨**信箱**，同一个理由再往上一层：不记得落在哪个文件夹的人，
      // 更不记得落在哪个箱。所以这一条请求要报上手上开着的全部令牌——服务端
      // 没有「这个人有哪些令牌」的索引，范围只能由持有者报、由服务端逐把核。
      const d = await get<{
        hits: {
          mail: InboundMail
          folder: string
          matchSnippet: string
          accountId?: number | string
        }[]
        meta: { total: string }
        nextCursor: string
      }>('/mail-search', {
        page_size: pageSize,
        keyword: keyword.value,
        cursor,
      }, { headers: { 'X-Mail-Unlock-All': searchScopeHeader() } })
      // 过期的一次不许落盘，见 loadSeq。
      if (!fresh()) return false
      inbound.value = concatRows(append, inbound.value, (d.hits ?? []).map((h) => ({
        ...h.mail,
        // The text around the hit replaces the opening line: showing the
        // first sentence of a mail that matched on its fourth paragraph
        // makes the result look like a mistake.
        snippet: h.matchSnippet,
        matchFolder: h.folder,
        matchAccount: Number(h.accountId ?? 0),
      })))
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
        view: currentView.value,
        cursor,
        // 排序只在没有关键词时带：有关键词走的是搜索查询，服务端会拒绝
        // 在它上面排序（排序栏那时也不显示）。
        // 和 sortFields 用同一个判断（trim 过的）：只有空格的搜索框不算有
        // 关键词，否则排序栏显示着、参数却没带，点了「没反应」。
        ...(keyword.value.trim() ? {} : { sort_by: listSort.value.by, sort_dir: listSort.value.dir }),
        // 只看未读。和排序一样，有关键词时不带：那条路上服务端不接。
        ...(unreadOnly.value && !keyword.value.trim() ? { unread: '1' } : {}),
        // 看哪个信箱**不在这里传**：网关只认解锁令牌里的那个箱
        // （见 requireMailUnlock）。换箱是上面 currentAccount 那个 watch
        // 换令牌，不是换参数——传参数的话，退出 A 之后拿还活着的 B 的令牌
        // 配一个 accountId=A 照样读得到 A 的信。
      })
      // 过期的一次不许落盘，见 loadSeq。
      if (!fresh()) return false
      inbound.value = concatRows(append, inbound.value, d.mails ?? [])
      total.value = Number(d.meta?.total ?? 0)
      unreadCount.value = Number(d.unreadCount ?? 0)
      nextCursor.value = d.nextCursor ?? ''
    } else if (folder.value === 'drafts') {
      const d = await get<{ drafts: Draft[] }>('/email-drafts')
      // 过期的一次不许落盘，见 loadSeq。
      if (!fresh()) return false
      drafts.value = d.drafts ?? []
    } else if (folder.value === 'scheduled') {
      const d = await get<{
        sends: Scheduled[]
        meta: { total: string }
        nextCursor: string
      }>('/email-scheduled', {
        page_size: pageSize,
        cursor,
      })
      // 过期的一次不许落盘，见 loadSeq。
      if (!fresh()) return false
      scheduled.value = concatRows(append, scheduled.value, d.sends ?? [])
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
        cursor,
        sort_by: listSort.value.by,
        sort_dir: listSort.value.dir,
      })
      // 过期的一次不许落盘，见 loadSeq。
      if (!fresh()) return false
      mailboxSent.value = concatRows(append, mailboxSent.value, d.mails ?? [])
      total.value = Number(d.meta?.total ?? 0)
      nextCursor.value = d.nextCursor ?? ''
    } else if (folder.value === 'attention') {
      const d = await get<{
        messages: AttentionMessage[]
        meta: { total: string }
        nextCursor: string
      }>('/email-messages', {
        page_size: pageSize,
        keyword: keyword.value,
        attention_only: true,
        cursor,
      })
      // 过期的一次不许落盘，见 loadSeq。
      if (!fresh()) return false
      messages.value = concatRows(append, messages.value, d.messages ?? [])
      total.value = Number(d.meta?.total ?? 0)
      nextCursor.value = d.nextCursor ?? ''
      attentionCount.value = total.value
    } else {
      const d = await get<{ suppressions: Suppression[] }>('/email-suppressions', {
        keyword: keyword.value,
      })
      // 过期的一次不许落盘，见 loadSeq。
      if (!fresh()) return false
      suppressions.value = d.suppressions ?? []
    }
    if (append) loadedPages.value += 1
    return true
  } catch (e) {
    // 接下一页失败要说出来并停下：无限滚动最坏的坏法是**静静地停住**，
    // 屏幕上看着就是「到底了」，而其实还有几百封。重新拉整份列表的失败
    // 不在这里接——它有自己的提示路径（横幅、空状态）。
    if (append) {
      if (fresh()) moreFailed.value = true
      return false
    }
    throw e
  } finally {
    // 过期的一次连「不转圈了」都不该说：它清掉的可能是**新的那一次**正
    // 亮着的标志，于是观察器以为没人在取，又发一次。
    if (fresh()) {
      if (append) loadingMore.value = false
      else if (!opts.quiet) loading.value = false
    }
  }
}

// 重新拉，并把已经接出来的那几页拼回来。
//
// 一页一页地拼，不是一次要 N 页：游标是「上一页最后一行的位置」，中间那几页
// 只能顺着走。页数就是人自己滚出来的那几页，通常两三页。
//
// 任何一步没落盘就停：那说明这次重拉已经被更新的一次挤掉了（人又点了别的
// 文件夹），再往下拼就是往新名单上接旧数据。
async function reloadPages(opts: { quiet?: boolean } = {}): Promise<boolean> {
  const want = loadedPages.value
  if (!(await load({ ...opts, single: true }))) return false
  for (let i = 1; i < want; i++) {
    if (!nextCursor.value) break
    if (!(await load({ append: true, quiet: true }))) return false
  }
  return true
}

// A row click is a navigation; the route watcher does the fetching.
// 点开一封信**不动左栏**。
//
// 搜索结果横跨信箱，一开始这里会跟着切到那封信所在的箱——左边的高亮跟着
// 跳。那是错的：搜索是「翻遍所有箱找那封信」，不是「换个箱重新开始翻」。
// 左栏跳来跳去等于每点一条结果就换一次上下文，而人只是想看看这几封是什么。
//
// 那次切箱本来要解决的是「回信从哪个地址发出去」——不解决的话，读的是 B
// 收到的信而写信框的发件人还跟着左边高亮的 A，「读 B 的信、从 A 回过去」。
// 现在那件事由**这封信自己**回答：单封读取会带回 accountId，写信框和
// replyingAddress 都读它。信箱跟着信走，左栏跟着人走，两件事分开。
function openInbound(row: MailRow) {
  pushState({ mail: row.id })
}

// 双击一行：这封信自己开一个窗口。
//
// 照 Foxmail、Outlook、Apple Mail——桌面邮件客户端双击一封信都是这个意思。
// 单击已经把信摆在右边了，双击要的是另一件事：把它拿出这三栏，占满一块屏幕
// 去看，或者摆到第二块屏幕上，一边看信一边在主窗口里填单子。
//
// window.open 而不是新标签页：一封信是一个窗口，不是一页网站。给窗口起名
// 'mail-<id>'，同一封信双击第二次是把已经开着的那个拿到前面来，不是再开
// 一个一模一样的。
//
// **特意没写 noopener**，尽管几乎所有 window.open 都该写它：按 HTML 规范，
// 带 noopener 时浏览器必须新开一个互不相干的上下文，窗口名字整个被忽略
// ——于是上面那条「第二次是拿到前面来」就没了，双击十次开十个窗口。
// 这里不写它是安全的：开的是本站自己的 /mail/<id>，同源，不存在把
// window.opener 交给外站的问题。
//
// 投递记录（kind === 'ERP'）没有这个窗口：邮箱服务器上没有这封信，
// /inbound-mails/<id> 那条路上什么都没有。双击它就只是点了两下。
//
// 弹窗拦截器不会拦：这是双击直接触发的，浏览器认这是人的动作。
function openMailWindow(row: MailRow) {
  if (row.kind === 'ERP') return
  const url = router.resolve({ path: `/mail/${row.id}` }).href
  window.open(url, `mail-${row.id}`, 'width=1040,height=860')
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
    // 后台把办公文档先转好。不 await：正文和会话该立刻显示，预热是顺带的。
    void warmAttachmentPreviews(openedInbound.value)
  } catch {
    // A dead link — deleted mail, somebody else's id — falls back to the
    // list rather than a blank page.
    pushState({ mail: '' })
  }
}

// The conversation around it, fetched after the mail itself is already on
// screen. Only a real exchange (more than this one message) switches the
// page into thread mode; the newest turn arrives expanded, the rest
// collapsed to one line each.
async function loadThread(mail: InboundMail) {
  threadItems.value = []
  expandedThread.value = new Set()
  expandedTurnDetails.value = new Set()
  if (!mail.threadKey) return
  try {
    // id 一起带上：一个人可以绑多个信箱，同一条会话可能同时落在两个箱里
    // （客户抄送了两个地址）。列表按信箱分行，打开时不说明是从哪一封点进
    // 来的，就会把两个箱的同名会话合起来读——列表写着 (2)，进去是 4 封。
    const tr = await get<{ items: ThreadItem[] }>('/mail-threads', {
      key: mail.threadKey,
      id: mail.id,
    })
    if ((tr.items ?? []).length > 1) {
      threadItems.value = tr.items
      // **只展开最新的那一条**，不是"点进来的那一条"。
      //
      // 绝大多数时候两者是同一条：列表一行代表一条会话，显示的就是最新那封。
      // 但从搜索结果、从附件条、从别处跳进来时点的可能是中间某一封，那时
      // 展开的是它，而人想先看的是最后发生了什么。会话按时间正序，所以是
      // 最后一条。
      const newest = tr.items[tr.items.length - 1]
      expandedThread.value = new Set([`${newest.direction}:${newest.id}`])
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
  // counterparty 在两个方向上不是同一件事（我发出的那行是收件人，收到的那行
  // 是发件人），所以界面上不再直接显示它。下面四个两腿含义一致。
  counterparty: string
  who: string
  at: string
  fromEmail?: string
  fromName?: string
  toAll?: string
  cc?: string
  attachments?: {
    id: string
    fileName: string
    fileSize: number | string
    contentType?: string
    downloadUrl?: string
    previewUrl?: string
    previewKind?: string
    stored?: boolean
  }[]
}
const threadItems = ref<ThreadItem[]>([])
// 界面上最新的排最前。
//
// **只翻显示，不翻数据。** threadItems 保持服务端给的时间正序，因为「最新的
// 是最后一条」这个约定被好几处依赖着（默认展开哪一条、附件按发生顺序摊平），
// 而一条会话本身就是按时间发生的——把顺序倒进数据里，后面每一个读它的人都
// 要先想一遍「这里到底是正序还是倒序」。
const threadForDisplay = computed(() => [...threadItems.value].reverse())

// 整条会话的附件，摊平成一条。**跟着屏幕上的顺序走，不跟着数据的顺序走**
// ——所以摊的是 threadForDisplay 而不是 threadItems，最新那封的附件排最前。
//
// 从前这里摊的是 threadItems（时间正序），注释还写着「文件的顺序就是对话的
// 顺序」。那句话在会话改成倒序显示（#400）之后就不成立了：同一屏里，上面这
// 条附件条最早的在前，底下的邮件最新的在前，两个方向。点一个文件是要跳到它
// 所在的那一封去的，而人得先在两个相反的排法之间对一次位置。
//
// 一封信内部的几个附件不动：它们之间没有时间先后，原样就是发件人放的顺序。
const threadFiles = computed(() =>
  threadForDisplay.value.flatMap((item) =>
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

// 每一封自己记着详情开没开。一个 Set 而不是一个布尔：会话里同时摊开两封、
// 对着看发件人，正是要查这个的时候会做的事。
const expandedTurnDetails = ref<Set<string>>(new Set())

function isTurnDetailsOpen(it: ThreadItem) {
  return expandedTurnDetails.value.has(threadItemKey(it))
}

function toggleTurnDetails(it: ThreadItem) {
  const k = threadItemKey(it)
  const next = new Set(expandedTurnDetails.value)
  if (next.has(k)) next.delete(k)
  else next.add(k)
  expandedTurnDetails.value = next
}

// 一封信的详情，和单封阅读页那份是同一套说法。
//
// 这里能给的比单封那页少：会话的两腿来自两张表，我们发出去的那张表上没有
// 抄送、没有 SPF/DKIM、没有原件大小。少的就不列——列一个空行等于说「这封信
// 没有抄送」，而实际是「我们没存」，那是在替这封信断言一件不知道的事。
function turnDetailRows(it: ThreadItem) {
  const rows: { k: string; v: string }[] = []
  const add = (k: string, v?: string) => {
    if (v) rows.push({ k, v })
  }
  const from = turnSenderEmail(it)
  if (from) add(t('emails.detail.from'), `${it.fromName ? it.fromName + ' ' : ''}<${from}>`)
  add(t('emails.detail.to'), turnRecipients(it))
  add(t('emails.detail.cc'), it.cc)
  add(t('emails.detail.subject'), it.subject)
  if (it.at) add(t('emails.detail.sentAt'), zonedStamp(it.at))
  return rows
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
// 废纸篓里标记已读没有意义，已发送里也没有——见工具条上那两个按钮的注释。
// 草稿箱同理，而且更彻底：草稿根本没有已读未读这个属性，它是自己写的。
const canBulkRead = computed(
  () => folder.value !== 'trash' && folder.value !== 'sent' && folder.value !== 'drafts',
)
// 能挪的和单封那个「移动到」同一口径：收件箱、星标、归档和自建文件夹里的信。
const canBulkMove = computed(
  () => folder.value === 'inbox' || folder.value === 'starred' || folder.value === 'archive' || isCustomFolderKey(folder.value),
)

// 屏幕上这一批能勾的行。哪个文件夹取哪份数据由 mailSelection 决定，不在这里
// 各算各的——「已选 N 封」拿 inbound 算、而已发送用的是 mailboxSent，正是原来
// 在已发送里勾了没反应的原因。
const selectable = computed(() =>
  selectableRows(folder.value, {
    inbound: inbound.value,
    sent: mailboxSent.value,
    drafts: draftRows.value,
  }),
)
// Only rows still on screen count. A selection that survived a folder change
// or a page turn would act on mail the person can no longer see.
const pickedRows = computed(() => pickedRowsOf(selectable.value, picked.value))
const pickState = computed(() => selectAllState(selectable.value, picked.value))
const allPicked = computed(() => pickState.value.all)
const somePicked = computed(() => pickState.value.some)

function toggleAllPicked() {
  // Partial counts as "on" for this purpose: with some ticked, the obvious
  // meaning of clicking the box is "never mind", not "and the rest too".
  picked.value = toggleAll(selectable.value, picked.value)
}

// A new list means a new set of things to choose from.
watch([folder, () => inbound.value, () => mailboxSent.value, () => drafts.value], () => {
  if (picked.value.length) picked.value = []
})

// 左边不再是「这个箱的草稿箱」了，右边那封草稿就收起来。它不在 URL 里
// （见模板上的注释），所以没有别的东西会替它清场。什么算「不再是」以及
// 为什么不能在这儿列触发条件，见 lib/draftRow 里那段注释。
const draftContext = computed(() =>
  draftPreviewContext(currentAccount.value, folder.value, isSearching.value),
)
watch(draftContext, () => {
  if (openedDraft.value) openedDraft.value = null
})

// ------------------------------------------------- 表格类文件夹的勾选 ---
// 定时 / 异常 / 免打扰名单三个用的是 el-table，不是邮件列表。它们的全选
// 交给 el-table 自己的选择列——这几个列表**有表头**，表头上的框是所有人都认得
// 的位置。邮件列表没有表头，所以它的全选框只能待在工具条上（见上面的注释）：
// 位置不同不是不一致，是各自跟着自己的形状走。
//
// 草稿箱从前也在这一组里，现在走上面那套（picked）：它已经是邮件列表了。
//
// 一次只渲染一个文件夹，所以一个 ref 装得下。
type TableRow = Scheduled | AttentionMessage | Suppression
const tablePicked = ref<TableRow[]>([])
const tableRef = ref()

function onTableSelect(rows: TableRow[]) {
  tablePicked.value = rows
}

// el-table 把勾选框那一格的点击也算成点了这一行。不挡住的话，勾一行就会顺手
// 把它打开——勾选是为了先挑出几行再一起处理，打开是相反的方向。
function notSelectionCell(col?: { type?: string }) {
  return col?.type !== 'selection'
}

function openMessageRow(row: AttentionMessage, col?: { type?: string }) {
  if (notSelectionCell(col)) openMessage(row)
}

function clearTablePick() {
  tablePicked.value = []
  tableRef.value?.clearSelection()
}

// 换文件夹、或者列表重新拉过一遍，勾选就作废：el-table 认的是行对象本身，
// 刷新后拿到的是一批新对象，它那边已经清空了，这边不清就成了一份操作不了的
// 幽灵勾选。
watch(
  [folder, () => scheduled.value, () => messages.value, () => suppressions.value],
  () => {
    if (tablePicked.value.length) tablePicked.value = []
  },
)

async function bulkDropDrafts() {
  const rows = pickedRows.value
  if (!rows.length) return
  await ElMessageBox.confirm(t('emails.dropDraftsHint', { n: rows.length }), common('delete'), {
    type: 'warning',
  })
  bulkBusy.value = true
  try {
    const failed = await inChunks(rows, (r) => del(`/email-drafts/${r.id}`, undefined, quietErrors))
    reportBulk(rows.length, failed, t('emails.draftsDropped', { n: rows.length - failed }))
  } finally {
    picked.value = []
    // 右边正看着的那封可能刚被删掉。留着的话删完还挂在那儿，点「接着写」
    // 才发现它已经不在了。
    if (openedDraft.value && rows.some((r) => r.id === openedDraft.value?.id)) closeDraft()
    bulkBusy.value = false
    load()
    loadDraftCount()
  }
}

// 只做批量取消，不做批量「立即发送」。取消能反悔——邮件回到草稿箱；而一次把
// 十个定时任务提前放出去是发给客户的、收不回来的。
async function bulkCancelScheduled() {
  const rows = tablePicked.value as Scheduled[]
  if (!rows.length) return
  await ElMessageBox.confirm(
    t('emails.cancelSchedulesAsk', { n: rows.length }),
    t('emails.cancelSchedule'),
    { type: 'warning' },
  )
  bulkBusy.value = true
  try {
    const failed = await inChunks(rows, (r) =>
      post(`/email-scheduled/${r.campaignId}/cancel`, undefined, quietErrors),
    )
    reportBulk(rows.length, failed, t('emails.cancelSchedulesDone', { n: rows.length - failed }))
  } finally {
    clearTablePick()
    bulkBusy.value = false
    load()
    loadDraftCount()
  }
}

// 一个原因写一次，套在选中的每一封上。原因是留给日后查证的，逐封问一遍只会
// 让人把同一句话敲十遍——那样写出来的原因也不会更准。
async function bulkAbandon() {
  const rows = tablePicked.value as AttentionMessage[]
  if (!rows.length) return
  const { value } = await ElMessageBox.prompt(
    t('emails.abandonManyHint', { n: rows.length }),
    t('emails.abandonTitle'),
    {
      inputPlaceholder: t('emails.abandonReason'),
      inputPattern: /\S/,
      inputErrorMessage: t('emails.abandonReason'),
    },
  )
  bulkBusy.value = true
  try {
    const failed = await inChunks(rows, (r) =>
      post(`/email-messages/${r.id}/abandon`, { reason: value }, quietErrors),
    )
    reportBulk(rows.length, failed, t('emails.abandonedN', { n: rows.length - failed }))
  } finally {
    clearTablePick()
    bulkBusy.value = false
    load()
    refreshAttentionCount()
  }
}

async function bulkUnsuppress() {
  const rows = tablePicked.value as Suppression[]
  if (!rows.length) return
  await ElMessageBox.confirm(
    t('emails.unsuppressManyHint', { n: rows.length }),
    t('emails.unsuppress'),
    { type: 'warning' },
  )
  bulkBusy.value = true
  try {
    const failed = await inChunks(rows, (r) =>
      del(`/email-suppressions?email=${encodeURIComponent(r.email)}`, undefined, quietErrors),
    )
    reportBulk(rows.length, failed, t('emails.unsuppressedN', { n: rows.length - failed }))
  } finally {
    clearTablePick()
    bulkBusy.value = false
    load()
  }
}

// Applies one change to everything ticked.
//
// One request per mail, deliberately: the server coalesces them where it
// counts — every queued flag change for the same account and folder leaves as
// a single IMAP STORE — so a batch endpoint would save round trips to our own
// gateway and nothing at the mail host. Sent a few at a time so twenty
// selected mails do not open twenty connections at once.
// 勾选多封后的「移动到」。一次请求，服务端按来源文件夹分组、一组一次 MOVE，
// 而不是像 bulkMark 那样逐封打接口：每封信各登录一次邮箱服务器，网易会限流。
// 整条会话一起挪（wholeThread）——列表一行就是一条会话，只挪最新那封会把
// 行留在原地、少一封。
// ---------------------------------------------------- 拖邮件到文件夹

// 手上正拖着的那几封，以及它们属于哪些信箱。左栏据此决定哪些文件夹亮起来。
//
// 放在页面这一层而不是靠 dataTransfer 传：dragover 里**读不到**
// dataTransfer 的内容（浏览器只在 drop 那一刻才交出来），而「这一格能不能
// 接」正是要在 dragover 里回答的。
const dragging = ref<{ ids: string[]; accounts: number[] } | null>(null)

function onDragMails(payload: { ids: string[]; accounts: number[] }) {
  dragging.value = payload
}

// 放下了。
//
// 两种落法，走的都是**已经存在的**那条路——拖拽是同一件事的又一个入口，
// 不是另一套后端：
//
//   挪 —— 和「移动到」下拉、批量工具条同一个 /inbound-mails/move
//   标 —— 和阅读区的「归档 / 删除」同一个 /inbound-mails/{id}/mark
//
// 为什么不能都用「挪」：视图看的是 deleted_at / archived_at 两个时间戳，
// 不是 folder。见 lib/dragMails 上那段。
async function onDropMails(_accountId: number, target: DropTarget) {
  const ids = dragging.value?.ids ?? []
  dragging.value = null
  if (!ids.length || bulkBusy.value) return
  bulkBusy.value = true
  try {
    if (target.kind === 'move') {
      const d = await post<{ moved?: number | string; failedIds?: string[] }>('/inbound-mails/move', {
        ids,
        folderId: String(target.folderId),
        wholeThread: true,
      })
      const moved = Number(d.moved ?? 0)
      const failed = d.failedIds?.length ?? 0
      if (failed === 0) ElMessage.success(t('emails.bulkMoved', { n: moved }))
      else ElMessage.warning(t('emails.bulkMovedPartial', { n: moved, failed }))
    } else {
      // 一封一条请求，和 bulkMark 同一个做法：标记接口是按单封设计的，
      // inChunks 控着并发不把网关打满。
      const failed = await inChunks(ids, (id) =>
        post(`/inbound-mails/${id}/mark`, { ...target.flags, wholeThread: true }, quietErrors),
      )
      reportBulk(ids.length, failed, t('emails.bulkDone', { n: ids.length - failed }))
    }
  } catch {
    // 后端的原因拦截器已经弹了
  } finally {
    // 只清掉刚处理的那几封，不清整份勾选：拖的可能是一封没勾的，那时勾选
    // 里还留着别的信，替人清掉是替人做决定。
    picked.value = picked.value.filter((id) => !ids.includes(id))
    bulkBusy.value = false
    load()
    refreshUnread()
  }
}

async function bulkMoveTo(folderId: number) {
  const rows = pickedRows.value
  if (!rows.length || bulkBusy.value) return
  bulkBusy.value = true
  try {
    const d = await post<{ moved?: number | string; failedIds?: string[] }>('/inbound-mails/move', {
      ids: rows.map((r) => r.id),
      folderId: String(folderId),
      wholeThread: true,
    })
    const moved = Number(d.moved ?? 0)
    const failed = d.failedIds?.length ?? 0
    if (failed === 0) ElMessage.success(t('emails.bulkMoved', { n: moved }))
    else ElMessage.warning(t('emails.bulkMovedPartial', { n: moved, failed }))
  } catch {
    // 后端的原因拦截器已经弹了
  } finally {
    picked.value = []
    bulkBusy.value = false
    load()
    refreshUnread()
  }
}

async function bulkMark(flags: Record<string, boolean>) {
  const rows = pickedRows.value
  if (!rows.length) return
  bulkBusy.value = true
  try {
    const failed = await inChunks(rows, (row) =>
      post(`/inbound-mails/${row.id}/mark`, { ...flags, wholeThread: true }, quietErrors),
    )
    reportBulk(rows.length, failed, t('emails.bulkDone', { n: rows.length - failed }))
  } finally {
    // 无论成没成都要刷新。做了一半的时候列表还照着旧样子摆着，是最难查的一种
    // ——人看到的和实际发生的对不上，而且没有任何提示说它们对不上。
    picked.value = []
    bulkBusy.value = false
    load()
    refreshUnread()
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
    const failed = await inChunks(rows, (row) =>
      del(`/inbound-mails/${row.id}?whole_thread=true`, undefined, quietErrors),
    )
    reportBulk(rows.length, failed, t('emails.purgedN', { n: rows.length - failed }))
  } finally {
    picked.value = []
    bulkBusy.value = false
    load()
  }
}

const bulkConcurrency = 4

/**
 * 分批跑完，返回**失败了几条**。
 *
 * 用 allSettled 而不是 all：一批里有一条失败，all 会让整批一起中断、后面几批
 * 干脆不跑——于是「删掉了 3 封、剩下 17 封没动」这件事既没人说，列表也停在原样。
 *
 * 每一条自己不弹错（quietErrors），由 reportBulk 汇总成一句话。二十条失败弹
 * 二十个红条，是把同一件事说了二十遍，反而看不出到底成了几条。
 */
async function inChunks<T>(rows: T[], run: (row: T) => Promise<unknown>): Promise<number> {
  let failed = 0
  for (let i = 0; i < rows.length; i += bulkConcurrency) {
    const settled = await Promise.allSettled(rows.slice(i, i + bulkConcurrency).map(run))
    failed += settled.filter((r) => r.status === 'rejected').length
  }
  return failed
}

/** 批量操作之后说一句实话：全成了，还是有几条没成。 */
function reportBulk(total: number, failed: number, doneMsg: string) {
  if (failed === 0) {
    ElMessage.success(doneMsg)
    return
  }
  ElMessage.warning(t('emails.bulkPartial', { done: total - failed, failed }))
}

// markRow / purgeRow 随列表行内按钮一起去掉了：现在列表上没有单封操作，
// 那些动作在右边阅读区的工具条上（markOpened / purgeOpened），批量的走
// 勾选框加上面那条工具条（bulkMark / bulkPurge）。

async function toggleStar(row: MailRow) {
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

// 回复全部：收件人是发信人（有 Reply-To 用它），原信 To 和 Cc 里其余的人进
// 抄送，去掉我名下全部信箱的地址。规则和测试在 lib/replyAll。
// 这次回信会从哪个地址发出去。写信框的发件人读的是同一个来源
// （composeAccount → EmailComposer 的 fromAccount），所以两边不会各说各的。
function replyingAddress(): string {
  const boxes = mailboxes.value
  const cur = boxes.find((b) => b.id === composeAccount.value)
  return (cur ?? boxes.find((b) => b.isDefault) ?? boxes[0])?.email ?? ''
}

async function replyAllToInbound() {
  if (!openedInbound.value) return
  composing.value = true
  await nextTick()
  const m = openedInbound.value
  composer.value?.openReplyAll(m, replyAllRecipients({ ...m, self: replyingAddress() }))
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
      `/inbound-mails/mark-view-read?view=${encodeURIComponent(currentView.value)}`,
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
//
// 横幅说的是**当前这个信箱**。一个人有好几个箱时，「我的邮箱出错了」这句
// 话必须指得出是哪一个——从前它读的是按人取单行的那个答案，两个箱时那个
// 答案是随机的，横幅可能在说另一个箱的事。左侧每个箱自己还有一个红点。
async function checkSyncHealth() {
  try {
    const d = await get<{ accounts?: { id: number; lastError: string; needsReauth?: boolean; email: string }[] }>(
      '/my-mailboxes',
    )
    const mine = d.accounts ?? []
    const cur = mine.find((a) => Number(a.id) === currentAccount.value) ?? mine[0]
    syncBanner.value = buildSyncBanner({ lastError: cur?.lastError, needsReauth: cur?.needsReauth })
    myAddresses.value = new Set(
      mine.map((a) => (a.email ?? '').trim().toLowerCase()).filter(Boolean),
    )
  } catch {
    /* the banner is a courtesy; its absence must not break the page */
  }
}

// 这个部署有没有接模型适配器。**只问一次**：它是部署配置，不会在一个人
// 开着页面的时候变，而 checkSyncHealth 是一分钟一轮的——顺带问它等于每分钟
// 多一次往返，一整天几百次，答案永远一样。
async function loadExcelCapability() {
  try {
    const d = await get<{ excelAvailable: boolean }>('/my-mail-account')
    excelAvailable.value = d.excelAvailable === true
  } catch {
    excelAvailable.value = false
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
    const d = await post<{ fetched: number; detail: string; needsReauth?: boolean }>(
      '/mailbox/sync',
      undefined,
      mailHostRequest,
    )
    if (d.detail) {
      syncBanner.value = buildSyncBanner({ detail: d.detail, needsReauth: d.needsReauth })
      return
    }
    syncBanner.value = { text: '', offerReauth: false }
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
    // 跟着**当前这个箱**问。不带的话答的是默认箱：263 是密码箱、Gmail 是
    // Google 箱，问错了就会在密码门前弹去 Google，或者反过来。
    const d = await get<{ account: { authKind: string; email: string } }>(
      '/my-mail-account',
      { accountId: currentAccount.value },
    )
    if (d.account?.authKind === 'OAUTH') {
      // Full-page departure, same as the gate: popups get blocked, and
      // Google's page is where the person should see themselves go.
      //
      // **带上地址。** 这是「某个箱的授权失效了，去续」那条路，网关那边
      // 专门为它留了这个参数（见 startGoogleOAuth 的注释）。不带的话
      // Google 弹出的账号选择器没有任何提示，而两个 Google 账号在那个
      // 列表里长得一模一样——**挑错一个就会把另一个箱的凭据覆盖掉**，
      // 而且是在人以为自己在修这个箱的时候。
      const r = await get<{ url: string }>('/oauth/google/start', {
        email: d.account?.email || '',
      })
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
  pushState({ folder: 'sent', page: 1, q: '', sent: 'erp', mail: '', sort: '' })
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
// 附件在附件条上的样子，以 MailAttachments 的定义为准——那是唯一渲染它
// 的地方。这里原本自己从 InboundMail 推了一个同名类型出来，两个 MailFile
// 差在 contentType 是不是必填，于是传给组件的回调一直是对不上的。

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

interface SourcingCustomerContact {
  id: string
  name: string
  department: string
  title: string
  email: string
  isPrimary: boolean
}

type ExcelSource =
  | { kind: 'text'; mailId: string; text: string }
  | { kind: 'attachment'; mailId: string; attachmentId: string }

const excelMenu = reactive({
  open: false, x: 0, y: 0, source: null as ExcelSource | null, disabledReason: '',
})
const customerCreateOpen = ref(false)
const excelOpen = ref(false)
const excelTemplateOpen = ref(false)
const excelTemplatesBusy = ref(false)
const excelTemplates = ref<InquiryTemplate[]>([])
const selectedInquiryTemplateId = ref('')
const pendingExcelSource = ref<ExcelSource | null>(null)
const selectedExcelTemplate = computed(() =>
  excelTemplates.value.find((template) => template.id === selectedInquiryTemplateId.value) ?? null,
)
const excelBusy = ref(false)
const excelResult = ref<ExcelResult | null>(null)
const excelJobId = ref('')
const excelSheet = ref('')
const excelAvailable = ref(false)
const creatingSourcingCase = ref(false)
const sourcingOpen = ref(false)
const sourcingContactsLoading = ref(false)
const sourcingContacts = ref<SourcingCustomerContact[]>([])
// 转入采购必须关联主数据中的客户和联系人，姓名与邮箱只作为后端保存的快照。
const sourcingForm = reactive({ customerId: '', customerName: '', contactId: '' })
const selectedSourcingContact = computed(() =>
  sourcingContacts.value.find((contact) => contact.id === sourcingForm.contactId) ?? null,
)
const convertedExcelSource = ref<ExcelSource | null>(null)
// Results live in memory: asking for the same attachment or text again opens
// the stored workbook instead of spending another model call. 重新生成 is the
// explicit way to pay for a fresh read.
const excelResultCache = new Map<string, ExcelResult>()
// 计时器句柄写死成 number，不写 ReturnType<typeof setTimeout>：测试要用
// node:zlib，于是 node 的类型进了全局，而 Node 的 setTimeout 返回的是
// Timeout 对象、浏览器的返回 number。这三处跑在浏览器里，number 才是它
// 们真正的样子——推导反而会挑错那一版。
let excelPollTimer: number | null = null

function excelCacheKey(source: ExcelSource, templateId = selectedInquiryTemplateId.value): string {
  const sourceKey = source.kind === 'attachment'
    ? `attachment:${source.mailId}:${source.attachmentId}`
    : `text:${source.mailId}:${source.text}`
  return `${sourceKey}:template:${templateId || 'direct'}`
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

let excelHoverTimer: number | null = null
let excelHideTimer: number | null = null

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

function openCustomerFromSender() {
  if (!canCreateCustomerFromSender.value) return
  customerCreateOpen.value = true
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
  // 有模型时先选模板，即使附件本身是 xlsx 也要按所选模板标准化。没有模型
  // 时仍保留原表直接预览，避免破坏既有的基础查看能力。
  const directFile = directTableAttachment(source)
  if (!excelAvailable.value) {
    if (directFile && (await openAttachmentDirect(directFile))) return
    ElMessage.error(t('emails.excelUnavailable'))
    return
  }
  await openExcelTemplatePicker(source)
}

async function loadExcelTemplates() {
  excelTemplatesBusy.value = true
  try {
    const data = await get<{ templates: InquiryTemplate[] }>('/mail-inquiry-templates', undefined, mailExcelRequest)
    excelTemplates.value = (data.templates ?? []).filter((template) => template.status === 'ACTIVE')
    const selectedStillExists = excelTemplates.value.some((template) => template.id === selectedInquiryTemplateId.value)
    if (!selectedStillExists) {
      selectedInquiryTemplateId.value = excelTemplates.value.find((template) => template.isDefault)?.id
        ?? excelTemplates.value[0]?.id
        ?? ''
    }
  } finally {
    excelTemplatesBusy.value = false
  }
}

async function openExcelTemplatePicker(source: ExcelSource) {
  pendingExcelSource.value = source
  excelTemplateOpen.value = true
  try {
    await loadExcelTemplates()
  } catch {
    excelTemplateOpen.value = false
  }
}

async function confirmExcelTemplate() {
  const source = pendingExcelSource.value
  const templateId = selectedInquiryTemplateId.value
  if (!source || !templateId || excelTemplatesBusy.value) return
  excelTemplateOpen.value = false
  const cached = excelResultCache.get(excelCacheKey(source, templateId))
  if (cached) {
    convertedExcelSource.value = source
    excelResult.value = cached
    excelSheet.value = cached.sheets[0]?.name ?? ''
    excelOpen.value = true
    return
  }
  await startExcelConversion(source, templateId)
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
    excelResultCache.set(excelCacheKey(source, 'direct'), result)
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

async function startExcelConversion(source: ExcelSource, templateId = selectedInquiryTemplateId.value) {
  excelResult.value = null
  convertedExcelSource.value = source
  excelSheet.value = ''
  excelOpen.value = true
  excelBusy.value = true
  try {
    const body = source.kind === 'text'
      ? { selectedText: source.text, locale: locale.value, inquiryTemplateId: templateId }
      : { attachmentId: source.attachmentId, locale: locale.value, inquiryTemplateId: templateId }
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
  excelOpen.value = false
  await openExcelTemplatePicker(source)
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
    if (job.inquiryTemplateId) selectedInquiryTemplateId.value = job.inquiryTemplateId
    excelResult.value = job.result
    if (convertedExcelSource.value) {
      excelResultCache.set(excelCacheKey(convertedExcelSource.value, job.inquiryTemplateId), job.result)
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
  sourcingForm.contactId = ''
  sourcingContacts.value = []
  sourcingOpen.value = true
}

function sourcingContactLabel(contact: SourcingCustomerContact) {
  const role = [contact.department, contact.title].filter(Boolean).join(' / ')
  const primary = contact.isPrimary ? ` · ${t('emails.sourcingPrimaryContact')}` : ''
  const email = contact.email || t('emails.sourcingContactEmailMissing')
  return `${contact.name}${role ? ` · ${role}` : ''} · ${email}${primary}`
}

async function selectSourcingCustomer(customer?: { id: string | number; name: string }) {
  sourcingForm.customerName = customer?.name || ''
  sourcingForm.contactId = ''
  sourcingContacts.value = []
  const customerId = String(customer?.id ?? sourcingForm.customerId ?? '')
  if (!customerId) {
    sourcingContactsLoading.value = false
    return
  }
  sourcingContactsLoading.value = true
  try {
    const data = await get<{ contacts: SourcingCustomerContact[] }>(`/sourcing-customer-options/${customerId}/contacts`)
    if (String(sourcingForm.customerId) !== customerId) return
    sourcingContacts.value = data.contacts ?? []
  } finally {
    if (String(sourcingForm.customerId) === customerId) sourcingContactsLoading.value = false
  }
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
  if (!sourcingForm.contactId) {
    ElMessage.warning(t('emails.sourcingContactRequired'))
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
      contactId: sourcingForm.contactId,
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
    router.push(`/sales/intakes?intake=${response.sourcingCase.id}`)
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

const convertingAttachment = ref('')
const bundling = ref(false)

/** 把这封信的附件打成一个压缩包下载。 */
async function downloadAllAttachments() {
  const id = openedInbound.value?.id
  if (!id || bundling.value) return
  bundling.value = true
  try {
    const file = await download(`/inbound-mails/${id}/attachments/download`)
    saveBlob(file.blob, file.fileName)
  } catch {
    // 具体原因（太大、原件读不到）后端已经用消息说了，拦截器会弹出来。
  } finally {
    bundling.value = false
  }
}

/**
 * 打开一封信之后，悄悄把要转换的附件先转好。
 *
 * 转换本身在服务器上只要 0.3 秒左右，但一次完整的往返（读原件、转换、写回
 * 对象存储、签地址）实测 1 到 1.8 秒——挂在「预览」这颗按钮上，人是等得到的。
 * 而人打开一封信到点开附件之间，通常有好几秒在读正文。这段时间白白空着。
 *
 * 所以在这里预热：转好的地址写回附件对象，等真点下去时 needsConversion 已经
 * 是 false，弹窗直接开。没点的那些就当白转了一次——服务器那边有缓存，下次
 * 谁点都是秒开，不算浪费。
 *
 * **一个一个来，不并发**：一封信可能带四十个附件，四十个请求同时压给转换器
 * 只会让每一个都变慢。也不抢在用户手动点的前面——那一次有人在等。
 */
async function warmAttachmentPreviews(mail: { id: string; attachments?: MailFile[] } | null) {
  if (!mail?.attachments?.length) return
  const opened = mail.id
  for (const a of mail.attachments) {
    // 用户已经自己点了某个附件，把转换器让给他。
    if (convertingAttachment.value) return
    // 翻到别的信上去了，这封就不用预热了。
    if (openedInbound.value?.id !== opened) return
    if (!needsConversion(a)) continue
    try {
      const resp = await post<{ previewUrl?: string }>(
        `/inbound-mails/${opened}/attachments/${a.id}/preview`,
        undefined,
        // 预热失败不该在页面上弹一句话——没人请求过它。
        quietErrors,
      )
      if (resp?.previewUrl) a.previewUrl = resp.previewUrl
    } catch {
      // 转不了的（坏文件、太大）等用户真点的时候再如实报错。
    }
  }
}

/**
 * 打开预览。
 *
 * 图片和 PDF 直接开。Word / Excel / PPT 要先请服务器转成 PDF——转换是按需的，
 * 不是每封信一到就把所有附件都转一遍：绝大多数附件没人点开。
 *
 * 转出来的地址写回这个附件对象，所以同一份文件第二次点是直接开的，连请求
 * 都不发。服务器那边也有缓存，换个人点同样不会重转。
 */
async function openPreview(a: MailFile, mailID: string) {
  if (!needsConversion(a)) {
    previewing.value = a
    previewOpen.value = true
    return
  }
  if (convertingAttachment.value) return
  if (!mailID) return
  convertingAttachment.value = a.id
  try {
    const resp = await post<{ previewUrl?: string }>(
      `/inbound-mails/${mailID}/attachments/${a.id}/preview`,
    )
    if (!resp?.previewUrl) throw new Error('no url')
    a.previewUrl = resp.previewUrl
    previewing.value = a
    previewOpen.value = true
  } catch {
    // 具体原因（类型不支持、文件太大、转换失败）后端已经用消息说了，
    // 拦截器会弹出来；这里不再叠一层。
  } finally {
    convertingAttachment.value = ''
  }
}

function isImage(a: MailFile) {
  return (a.contentType || '').toLowerCase().startsWith('image/')
}

// Why an attachment cannot be downloaded, said accurately.
//
// These two were one message once, and it told somebody their 8 MB deck had
// no copy kept when the file was fine and a stale service was dropping the
// field. A message that confident about somebody's data has to be earned.
// 和 MailAttachments 里那一句同源：判断在 lib/attachmentHint，这里只翻译。
// 原来这两处各写了一遍，改一处忘另一处就会出现「同一个附件两个地方说法不同」。
function fileHint(a: { fileName: string; downloadUrl?: string; stored?: boolean; fileSize?: number | string }) {
  return t(`emails.${attachmentHintKey(a)}`, { f: a.fileName })
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
function openSentRow(row: MailRow) {
  if (row.kind === 'ERP') {
    pushState({ msg: row.id })
    return
  }
  pushState({ mail: row.id })
}

function openMessage(row: AttentionMessage) {
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

function openRequeue(row: AttentionMessage) {
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

async function doAbandon(row: AttentionMessage) {
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
  /* 从前是 gap: 18px。现在两栏之间站着一条分隔条，那 18px 由「4 + 10 + 4」
     凑出来：留白没变，中间那 10px 成了能抓住的东西。 */
  gap: 4px;
  align-items: stretch;
  min-height: 100%;
  background: var(--el-bg-color);
}
.rail {
  flex: none;
  /* 比从前宽 30px：文件夹缩进到信箱底下之后，178px 里再去掉一层缩进，
     「拒收名单」这种四个字的名字就要被截断了。右边是 flex:1 的列表，
     它自己会让出来。 */
  width: 208px;
  /* Back to its own height, which stretch had just taken away — a sticky
     element as tall as its container has nowhere to stick to. */
  align-self: flex-start;
  position: sticky;
  top: 12px;
  /* 自己滚。信箱多、文件夹多的时候这一栏会比一屏长，而它 sticky 在顶上，
     长出去的部分原来只能靠整页滚动才够得着——那时候右边两栏也跟着走了。 */
  max-height: calc(100vh - 24px);
  overflow-y: auto;
  /* 滚动条的位置**一直留着**，不等它出现才腾。

     Windows 的 Chrome/Edge 用经典滚动条，占 15px 布局宽度；macOS 默认是
     覆盖式的，占 0。所以同一段代码在两个平台上表现不同，而这正是这条
     规则要修的事：

     广告信的图片常常只声明 width、不声明 height，加载完之前占 0 高度。
     信一打开是「只有文字」的高度，装得下、没有滚动条；图片一张张落地、
     内容长过一屏，滚动条**突然出现**，整列内容在一帧之内横向缩 15px。
     那就是 Windows 上「点开广告邮件抖一下」的真正原因——是横向的，不是
     纵向变高。实测：500px → 485px。

     stable 让这 15px 从一开始就留出来，出现与否都不影响布局。Mac 上覆盖式
     滚动条本来就占 0，所以留出来的也是 0，一点代价都没有（实测同样是
     500 → 500）。 */
  scrollbar-gutter: stable;
}
.compose {
  width: 100%;
  margin-bottom: 14px;
}
/* 搜索框在写信和信箱树之间。间距比 .compose 小一档：写信是这一栏的主按钮，
   搜索紧跟着它，两者是一组「我要做点什么」，和下面那棵「我的东西在哪儿」
   的树隔开。 */
.rail-search {
  margin-bottom: 12px;
}
.rail-search :deep(.el-input__prefix) {
  /* 放大镜按emoji渲染时基线偏高，压回文字中线。 */
  font-size: 13px;
  opacity: 0.65;
}
/* 三栏：文件夹 | 列表 | 阅读区。
   pane 自己竖着排，是为了让同步横幅横跨两列——那条横幅说的是整个信箱的
   状态，缩进任何一列都读着像只跟那一列有关。 */
.pane {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  /* One ground for everything on this side — the list, the four folders that
     are tables, the reading page, and the mail's own frame, which is given
     this same colour. The only white left on it is white that means
     something: a row under the cursor, and whatever card a sender drew.
     The padding keeps the content off the edge; it also narrows the container
     below, which is correct — the list should respond to the width it can
     actually use. */
  background: var(--mail-ground);
  padding: 14px 16px;
  /* 这里从前建着一个名叫 mailbox 的 CSS 容器，给邮件行的「窄了就收」用。
     **它已经删了**，连同那几条规则一起——见 MailList.vue 里那段说明。

     留个记号在这儿，因为这是个值得记住的坑：改成三栏之后 .pane 同时装着
     列表列和阅读区，容器测的是两列之和（一千多），而真正的列表列只有
     280–400px。规则一条都没触发，行里的东西溢出来叠在一起，看着像渲染
     出错。**容器建在哪一层，量的就是哪一层**，而布局改动会悄悄换掉那一层。 */
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
/* 列表上方那一条。从前顶着一行 19px 的文件夹名字，所以是"标题栏"；名字
   去掉之后它就只剩控件了，于是按工具条的尺寸来：里面的按钮全是 small，
   勾中邮件时换上的那排批量按钮也是 small——两种状态高度一样，勾一封信
   列表不会跳一下。 */
.pane-head {
  display: flex;
  align-items: center;
  gap: 8px;
  /* 挤不下就换行，不是把里面的字挤成一竖条。
     列表这一栏的宽度现在是人自己拖的，拖到 250px 也合理——那时这一条上的
     「排序：日期 ↓ / 只看未读 / 全部已读」放不下。放不下有两种办法：把每
     一样都压窄（于是「排序：日期」竖着排成三行，那正是这次要修的样子），
     或者整颗按钮挪到下一行。后者永远是对的：一颗按钮要么完整，要么不在。 */
  flex-wrap: wrap;
  row-gap: 6px;
  /* 换到第二行的按钮靠右，跟着第一行那几颗的右边缘走，不是散落在左边。
     第一行不受影响：那儿有一个 flex:1 的空档（.grow），free space 全被它
     吃掉了，justify-content 没得分配。 */
  justify-content: flex-end;
  min-height: 28px;
  margin-bottom: 8px;
}
.search-title {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
  /* 搜的词可以很长，而它不该把整条工具条挤走：超出就省略号，全词在左栏
     那个搜索框里原样摆着。 */
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
/* 排序：一颗不像按钮的按钮。它是这一条上最不重要的控件（一天点零到一次），
   画成实心按钮会和右边那两颗真按钮抢眼睛。 */
.sort-trigger {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  /* 不折行、不压缩：这五个字是一个整体，断在中间没有任何意义。 */
  flex: none;
  white-space: nowrap;
  border: 0;
  background: transparent;
  padding: 3px 10px;
  border-radius: 999px;
  font: inherit;
  font-size: var(--mail-meta);
  color: var(--el-text-color-secondary);
  cursor: pointer;
  transition: background var(--mail-fast) var(--mail-ease), color var(--mail-fast) var(--mail-ease);
}
.sort-trigger:hover {
  background: var(--el-fill-color);
  color: var(--el-text-color-primary);
}
.sort-trigger:focus-visible {
  outline: 2px solid var(--el-color-primary-light-5);
  outline-offset: 1px;
}
/* 菜单里"现在按的是这一项"那条规则在 styles/mailbox.css：下拉菜单是挂到
   <body> 上的，scoped 样式（连 :deep）都够不着它。 */
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
/* 哨兵本身没有高度也不占位：它只是给观察器一个「看得见了没」的靶子。 */
.more-sentinel {
  height: 1px;
}
.more-line {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 12px 0 18px;
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
  /* 一行装下：名字、地址、收件地址、详情、日期。gap 比从前小，因为这一行
     上东西多了；头像后面单独补一格。 */
  gap: 6px;
  min-width: 0;
}
.in-from .avatar {
  margin-right: 4px;
}
/* 三段字各自可缩，谁长谁先省略号。名字排在最前、缩得最少：一封信最先要
   回答的是「谁」。 */
.in-name {
  flex: 0 1 auto;
  min-width: 3em;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.in-addr,
.in-to {
  flex: 0 2 auto;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.sender-customer-action {
  flex: none;
  opacity: 0;
  pointer-events: none;
  transition: opacity 120ms ease;
}
.in-from:hover .sender-customer-action,
.in-from:focus-within .sender-customer-action {
  opacity: 1;
  pointer-events: auto;
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
  flex: none;
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
/* 阅读区的图标工具条。
   nowrap：整条加起来不到 160px，任何宽度都放得下——从前那排带文字的按钮
   会换行，第二排又和第一排左对齐，看着像两组不相干的东西。 */
.in-actions {
  display: flex;
  align-items: center;
  flex-wrap: nowrap;
  gap: 2px;
  margin-top: 14px;
}
.tb {
  display: grid;
  place-items: center;
  flex: none;
  min-width: 34px;
  height: 32px;
  padding: 0 6px;
  border: none;
  border-radius: 8px;
  background: none;
  color: var(--el-text-color-regular);
  /* 箭头和垃圾桶是字形，不是图标字体：字号大一档才和一行 14px 的正文
     视觉上等重。 */
  font-size: 17px;
  line-height: 1;
  cursor: pointer;
  transition: background var(--mail-fast) var(--mail-ease),
    color var(--mail-fast) var(--mail-ease);
}
.tb:hover {
  background: var(--el-fill-color);
  color: var(--el-text-color-primary);
}
.tb:focus-visible {
  outline: 2px solid var(--el-color-primary);
  outline-offset: -2px;
}
.tb-danger:hover {
  background: var(--el-color-danger-light-9);
  color: var(--el-color-danger);
}
.tb-sep {
  flex: none;
  width: 1px;
  height: 18px;
  margin: 0 6px;
  background: var(--el-border-color-lighter);
}
/* 菜单里那条「移动到」是组标题，不是可点的一项：压淡、去掉禁用态那种
   「本来能点、现在不能」的观感。 */
.menu-head {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  cursor: default;
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
.sync-retrying {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  white-space: nowrap;
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
/* 展开的那一封，正文之上的一行：完整发件地址 + 详情。和单封阅读页的头部
   同一个读法，只是窄一档。 */
.turn-meta {
  display: flex;
  align-items: baseline;
  flex-wrap: wrap;
  gap: 2px;
  margin: 0 0 8px;
  font-size: 13px;
}
.turn-meta .sub {
  min-width: 0;
  overflow-wrap: anywhere;
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

.att-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.att-head .side-title {
  margin: 0;
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
.sourcing-contact-help {
  margin-top: 6px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
  line-height: 1.45;
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
/* 纯文本信里的网址。看起来要像链接——不然人还是不会去点它，而这正是
   这次要修的那件事。见 lib/linkifyText。 */
.in-text a {
  color: var(--el-color-primary);
  text-decoration: none;
}
.in-text a:hover {
  text-decoration: underline;
}

.opened {
  color: var(--el-color-success);
  font-size: 12px;
  cursor: default;
}


/* **这里从前写的是 flex: 1，而它一直什么都没做**：.rail 不是 flex 容器
   （它自己是 .mailbox 的 flex item），所以这个 span 只是个不占地方的空标签。
   于是「退出」离上面那排只隔着下面那条 16px——想点模板点成退出，根子在这儿。
   改成一段实打实的间距。 */
.rail-grow {
  display: block;
  height: 18px;
}
/* 工具和退出之间的那条线。它不是装饰：上面是「做点什么」，下面是「离开」，
   两类动作贴在一起正是误触的来源。 */
.rail-sep {
  height: 1px;
  margin: 14px 12px 4px;
  background: var(--el-border-color-lighter);
}
.rail-signout {
  display: block;
}
.rail-lock {
  /* One function per line. These are inline-flex buttons by default, and
     four of them packing into whatever rows fit 178px read as clutter. */
  display: flex;
  width: fit-content;
  margin: 8px 0 0;
  /* 和文件夹、信箱那两块同一条左边界（12px）。不补的话它们贴着栏的最左边，
     而上面所有东西都缩进 12px——一条栏里两个左边界。 */
  padding-left: 12px;
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
  margin-top: 4px;
}

/* ---------------------------------------------------------------- 三栏 */
/* 列表和阅读区并排。**模板里阅读区写在列表前面**（原来是 v-if/v-else 的两
   个分支，谁在前无所谓），这里用 order 把列表拉到左边——比搬动三百多行
   模板安全得多，而且以后哪一栏要挪位置也只是改一个数字。 */
.panes {
  flex: 1;
  min-width: 0;
  display: flex;
  /* 同上：3 + 10 + 3 = 从前那 16px。 */
  gap: 3px;
  align-items: flex-start;
}
.list-col {
  order: 1;
  flex: 0 0 clamp(280px, 34%, 400px);
  min-width: 0;
  /* 三栏各滚各的。原来只有阅读区自己滚，列表跟着整页走——读一封长信时
     往下滚，左边的列表就被顶出视野，那正是三栏要避免的事。 */
  position: sticky;
  top: 12px;
  max-height: calc(100vh - 24px);
  overflow-y: auto;
  /* 同上：滚动条的位置一直留着，见 .rail 那段。 */
  scrollbar-gutter: stable;
}
.reader-col {
  order: 2;
  flex: 1 1 0;
  min-width: 0;
  /* 自己滚，别把整页拉长：左边列表要一直看得见，这正是三栏的意义。 */
  position: sticky;
  top: 12px;
  max-height: calc(100vh - 24px);
  overflow-y: auto;
  /* 同上：滚动条的位置一直留着，见 .rail 那段。 */
  scrollbar-gutter: stable;
}
/* 分隔条。
   平时什么都不画——三栏之间本来就该是留白，一条竖线摆在那儿是在把两块内容
   之间的关系说成"隔开"。鼠标压上来才出现一条细线，告诉你这儿能抓。
   10px 宽而线只有 2px：4px 的线抓不住（要瞄准），10px 的线看着像一栏。
   VS Code、Finder、Foxmail 都是这个做法。 */
.col-grip {
  flex: 0 0 10px;
  align-self: stretch;
  /* 自己是一整条可抓的区域，线画在正中间。 */
  background: linear-gradient(var(--el-border-color-lighter), var(--el-border-color-lighter))
    center / 2px 100% no-repeat;
  opacity: 0;
  cursor: col-resize;
  border-radius: 2px;
  /* 拖的时候别顺手滚动页面（触控板/触摸屏）。 */
  touch-action: none;
  transition: opacity var(--mail-fast) var(--mail-ease);
}
.col-grip:hover,
.col-grip:focus-visible,
.col-grip.grabbing {
  opacity: 1;
}
.col-grip.grabbing {
  background-image: linear-gradient(var(--el-color-primary), var(--el-color-primary));
}
.col-grip:focus-visible {
  outline: 2px solid var(--el-color-primary-light-5);
  outline-offset: -1px;
}
/* 列表和阅读区之间那条：和列表同一个 order，写在列表后面，所以画在它右边。
   不给 order 的话它默认是 0，会跑到最左边去——.panes 里的位置是 order 说
   了算的，DOM 顺序在那儿是反的（见 .panes 上面那段）。 */
.col-grip.for-list {
  order: 1;
}
/* 正在拖的时候，整页的光标都是那个左右箭头：指针已经被这条分隔条捕获了，
   鼠标压在别的东西上也还是在拖它，光标得说出这件事。 */
.mailbox:has(.col-grip.grabbing) {
  cursor: col-resize;
  user-select: none;
}

/* 没选信时右边说一句话。一片空白看着像坏了。 */
.reader-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 320px;
  color: var(--el-text-color-placeholder);
  font-size: 13px;
}

/* 窄屏退回从前那种「打开信就换页」：三栏挤在一起两边都读不了。
   900px 才退：三栏在 1000px 左右是能用的——Foxmail 就是在这个宽度下
   跑三栏的，它的列表列只有 200 出头。原来写 1180 是照着「列表固定 420」
   算的，而 420 本身就太宽了；列表改成按比例伸缩之后，这条线可以退到
   真正挤不下的地方。 */
@media (max-width: 900px) {
  .panes {
    display: block;
  }
  .list-col,
  .reader-col {
    flex: none;
    width: auto;
    position: static;
    max-height: none;
    overflow: visible;
  }
  /* 开着信就只显示信，没开就只显示列表——也就是改版之前的行为。 */
  .panes.has-open .list-col,
  .panes:not(.has-open) .reader-col {
    display: none;
  }
  .reader-empty {
    display: none;
  }
  /* 两栏上下叠着的时候没有"中间"：一条横着拖的竖线在这儿说不通。
     文件夹栏那条留着——.mailbox 在这个宽度下仍然是左右两块。 */
  .col-grip.for-list {
    display: none;
  }
}
</style>
