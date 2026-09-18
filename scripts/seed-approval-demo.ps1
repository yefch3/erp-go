param(
  [string]$BaseUrl = 'http://127.0.0.1:28382',
  [Parameter(Mandatory = $true)]
  [string]$TestPassword,
  [string]$AdminAccount = 'admin',
  [string]$AdminPassword = 'admin123'
)

$ErrorActionPreference = 'Stop'

function New-Session([string]$account, [string]$password) {
  $session = [Microsoft.PowerShell.Commands.WebRequestSession]::new()
  Invoke-RestMethod "$BaseUrl/api/auth/login" -Method Post -ContentType 'application/json' `
    -Body (@{ account = $account; password = $password } | ConvertTo-Json) -WebSession $session | Out-Null
  return $session
}

function Invoke-Api($session, [string]$method, [string]$path, $body = $null) {
  $headers = @{}
  foreach ($cookie in $session.Cookies.GetCookies([uri]$BaseUrl)) {
    if ($cookie.Name -eq 'erp_csrf') { $headers['X-CSRF-Token'] = $cookie.Value }
  }
  $args = @{
    Uri = "$BaseUrl/api$path"
    Method = $method
    WebSession = $session
    Headers = $headers
    ContentType = 'application/json'
  }
  if ($null -ne $body) { $args.Body = $body | ConvertTo-Json -Depth 20 -Compress }
  return (Invoke-RestMethod @args).data
}

function Find-Employee($admin, [string]$code) {
  $page = Invoke-Api $admin GET "/employees?keyword=$([uri]::EscapeDataString($code))&page_size=200"
  return @($page.employees | Where-Object { $_.code -eq $code } | Select-Object -First 1)
}

function Ensure-Employee(
  $admin,
  [string]$code,
  [string]$name,
  [string]$username,
  [string]$email,
  [string]$position,
  [string]$departmentID,
  [string]$managerID = '0'
) {
  $found = Find-Employee $admin $code
  if ($found.Count) { return $found[0] }

  $temporaryPassword = "Tmp!Copper#$([guid]::NewGuid().ToString('N').Substring(0, 12))"
  $created = (Invoke-Api $admin POST '/employees' @{
    code = $code
    name = $name
    departmentId = $departmentID
    position = $position
    email = $email
    phone = ''
    username = $username
    initialPassword = $temporaryPassword
    managerId = $managerID
    englishName = ''
    hireDate = '2026-09-18'
    remark = '第二阶段审批功能本地验收账号；可由脚本重复初始化。'
  }).employee

  # 管理员设置的初始密码必须由账号本人修改一次。这样测试账号登录后不会被
  # “必须修改密码”页面拦住，也不需要直接改 IAM 数据库。
  $owner = New-Session $username $temporaryPassword
  Invoke-Api $owner POST '/me/password' @{ oldPassword = $temporaryPassword; newPassword = $TestPassword } | Out-Null
  return $created
}

function Set-Roles($admin, $employee, [hashtable]$roleByCode, [string[]]$codes) {
  $ids = @($codes | ForEach-Object {
    if (!$roleByCode.ContainsKey($_)) { throw "Role $_ does not exist" }
    [string]$roleByCode[$_]
  })
  Invoke-Api $admin POST "/employees/$($employee.id)/roles" @{ roleIds = $ids } | Out-Null
}

function Ensure-Department($admin, [string]$leaderID) {
  $departments = @((Invoke-Api $admin GET '/departments').departments)
  $department = @($departments | Where-Object { $_.code -eq 'APPROVAL-DEMO' } | Select-Object -First 1)
  if (!$department.Count) {
    return (Invoke-Api $admin POST '/departments' @{
      code = 'APPROVAL-DEMO'
      name = '审批功能测试部'
      parentId = '1'
      sortOrder = 99
      leaderEmployeeId = $leaderID
    }).department
  }
  $current = $department[0]
  if ([string]$current.leaderEmployeeId -ne $leaderID) {
    return (Invoke-Api $admin PUT "/departments/$($current.id)" @{
      code = $current.code
      name = $current.name
      parentId = $current.parentId
      sortOrder = $current.sortOrder
      leaderEmployeeId = $leaderID
      status = $current.status
      expectedVersion = $current.version
    }).department
  }
  return $current
}

function Ensure-Manager($admin, $employee, [string]$managerID) {
  if ([string]$employee.managerId -ne $managerID) {
    Invoke-Api $admin POST "/employees/$($employee.id)/manager" @{ managerId = $managerID } | Out-Null
  }
}

function Ensure-EmployeeDepartment($admin, $employee, [string]$departmentID) {
  $current = (Invoke-Api $admin GET "/employees/$($employee.id)").employee
  if ([string]$current.departmentId -eq $departmentID) { return $current }
  return (Invoke-Api $admin PUT "/employees/$($current.id)" @{
    code = $current.code
    name = $current.name
    englishName = $current.englishName
    departmentId = $departmentID
    position = $current.position
    email = $current.email
    phone = $current.phone
    managerId = $current.managerId
    hireDate = $current.hireDate
    leaveDate = $current.leaveDate
    remark = $current.remark
    expectedVersion = $current.version
  }).employee
}

function Find-Claim($session, [string]$purpose) {
  $items = @((Invoke-Api $session GET '/travel-reimbursements').items)
  return @($items | Where-Object { $_.purpose -eq $purpose } | Select-Object -First 1)
}

function New-ClaimWithEvidence($session, [string]$purpose, [string]$amount) {
  $claim = (Invoke-Api $session POST '/travel-reimbursements' @{
    tripStart = '2026-09-18'
    tripEnd = '2026-09-19'
    origin = '上海'
    destination = '宁波'
    purpose = $purpose
    amount = $amount
    currency = 'CNY'
    paymentAccount = '审批演示测试账户'
    note = '用于第二阶段页面内审批验收，可安全保留在本地测试环境。'
  }).reimbursement

  $fileName = "approval-demo-$($claim.id).txt"
  $signed = Invoke-Api $session POST "/travel-reimbursements/$($claim.id)/files/presign" @{
    fileName = $fileName
    category = 'INVOICE'
  }
  $payload = [Text.Encoding]::UTF8.GetBytes("Approval demo evidence for reimbursement $($claim.id)")
  Invoke-WebRequest $signed.uploadUrl -Method Put -Body $payload -ContentType 'text/plain' -UseBasicParsing | Out-Null
  Invoke-Api $session POST "/travel-reimbursements/$($claim.id)/files" @{
    objectKey = $signed.objectKey
    fileName = $fileName
    category = 'INVOICE'
  } | Out-Null
  return $claim
}

function Ensure-SubmittedClaim($submitter, [string]$purpose, [string]$amount) {
  $found = Find-Claim $submitter $purpose
  $claim = if ($found.Count) { $found[0] } else { New-ClaimWithEvidence $submitter $purpose $amount }
  if ($claim.status -eq 'DRAFT') {
    $claim = (Invoke-Api $submitter POST "/travel-reimbursements/$($claim.id)/submit" @{}).reimbursement
  }
  return $claim
}

function Wait-ApprovalTask($session, [string]$businessID) {
  for ($attempt = 0; $attempt -lt 20; $attempt++) {
    $todos = @((Invoke-Api $session GET '/approvals/todos?biz_type=TRAVEL_REIMBURSEMENT&page=1&page_size=100').todos)
    $match = @($todos | Where-Object { [string]$_.instance.bizId -eq $businessID } | Select-Object -First 1)
    if ($match.Count) { return $match[0] }
    Start-Sleep -Milliseconds 300
  }
  throw "Approval task for reimbursement $businessID was not delivered"
}

$admin = New-Session $AdminAccount $AdminPassword
$roles = @((Invoke-Api $admin GET '/roles?page_size=200').roles)
$roleByCode = @{}
foreach ($role in $roles) { $roleByCode[$role.code] = [string]$role.id }

$manager = Ensure-Employee $admin 'APPROVAL-MANAGER' '审批测试-原审批人' 'approval-demo-manager' `
  'approval-demo-manager@example.test' '审批经理' '1' '1'
Set-Roles $admin $manager $roleByCode @('SALES_MANAGER', 'PROCUREMENT_MANAGER', 'SHIPPING_MANAGER', 'FINANCE_MANAGER')

$department = Ensure-Department $admin '0'
$manager = Ensure-EmployeeDepartment $admin $manager ([string]$department.id)
# 新部门创建时还没有成员，所以先建部门、再把负责人调入部门，最后设置负责人。
$department = Ensure-Department $admin ([string]$manager.id)

$submitter = Ensure-Employee $admin 'APPROVAL-SUBMITTER' '审批测试-提交人' 'approval-demo-submitter' `
  'approval-demo-submitter@example.test' '业务提交人' ([string]$department.id) ([string]$manager.id)
Set-Roles $admin $submitter $roleByCode @('SALES', 'BUYER', 'LOGISTICS')
Ensure-Manager $admin $submitter ([string]$manager.id)

$unrelated = Ensure-Employee $admin 'APPROVAL-OBSERVER' '审批测试-无关员工' 'approval-demo-observer' `
  'approval-demo-observer@example.test' '普通员工' '1' '1'
Set-Roles $admin $unrelated $roleByCode @('SALES')
Ensure-Manager $admin $unrelated '1'

$submitterSession = New-Session 'approval-demo-submitter' $TestPassword
$managerSession = New-Session 'approval-demo-manager' $TestPassword
$observerSession = New-Session 'approval-demo-observer' $TestPassword

$draftPurpose = '审批演示-提交人草稿'
$draftFound = Find-Claim $submitterSession $draftPurpose
$draft = if ($draftFound.Count) { $draftFound[0] } else {
  (Invoke-Api $submitterSession POST '/travel-reimbursements' @{
    tripStart = '2026-09-20'; tripEnd = '2026-09-20'; origin = '上海'; destination = '苏州'
    purpose = $draftPurpose; amount = '188.00'; currency = 'CNY'
    paymentAccount = '审批演示测试账户'; note = '用于测试草稿编辑与提交。'
  }).reimbursement
}

$departmentPurpose = '审批演示-等待部门负责人审批'
$departmentClaim = Ensure-SubmittedClaim $submitterSession $departmentPurpose '680.50'

$financePurpose = '审批演示-等待财务负责人审批'
$financeClaim = Ensure-SubmittedClaim $submitterSession $financePurpose '1280.00'
if ($financeClaim.status -eq 'PENDING_DEPARTMENT_CONFIRMATION') {
  $firstTask = Wait-ApprovalTask $managerSession ([string]$financeClaim.id)
  Invoke-Api $managerSession POST "/approvals/tasks/$($firstTask.task.id)/act" @{
    action = 'APPROVE'
    comment = '审批演示：部门负责人已确认，保留在财务审批阶段。'
  } | Out-Null
  for ($attempt = 0; $attempt -lt 20; $attempt++) {
    $updated = Find-Claim $submitterSession $financePurpose
    if ($updated.Count -and $updated[0].status -eq 'PENDING_FINANCE_APPROVAL') { $financeClaim = $updated[0]; break }
    Start-Sleep -Milliseconds 300
  }
}

$departmentTask = Wait-ApprovalTask $managerSession ([string]$departmentClaim.id)
$adminAction = Invoke-Api $admin GET "/approvals/actionable-task?biz_type=TRAVEL_REIMBURSEMENT&biz_id=$($departmentClaim.id)"
$observerAction = Invoke-Api $observerSession GET "/approvals/actionable-task?biz_type=TRAVEL_REIMBURSEMENT&biz_id=$($departmentClaim.id)"
$observerVisible = @((Invoke-Api $observerSession GET '/travel-reimbursements').items | Where-Object { [string]$_.id -eq [string]$departmentClaim.id }).Count -gt 0

$result = [ordered]@{
  generatedAt = [DateTimeOffset]::Now.ToString('o')
  baseUrl = $BaseUrl
  accounts = @(
    @{ purpose = '超级管理员/代审批'; account = $AdminAccount; employee = '系统管理员'; note = '沿用现有管理员账号' }
    @{ purpose = '原审批人'; account = 'approval-demo-manager'; employee = $manager.name; id = $manager.id }
    @{ purpose = '提交人'; account = 'approval-demo-submitter'; employee = $submitter.name; id = $submitter.id }
    @{ purpose = '无关员工'; account = 'approval-demo-observer'; employee = $unrelated.name; id = $unrelated.id }
  )
  department = @{ id = $department.id; name = $department.name; leader = $manager.name }
  reimbursements = @(
    @{ purpose = '草稿编辑和提交'; id = $draft.id; claimNo = $draft.claimNo; status = $draft.status }
    @{ purpose = '原审批人审批/管理员代审批'; id = $departmentClaim.id; claimNo = $departmentClaim.claimNo; status = $departmentClaim.status; taskId = $departmentTask.task.id }
    @{ purpose = '财务审批页面'; id = $financeClaim.id; claimNo = $financeClaim.claimNo; status = $financeClaim.status }
  )
  checks = @{
    adminCanOverride = [bool]$adminAction.override
    adminTaskId = $adminAction.task.id
    observerCannotSeeClaim = !$observerVisible
    observerHasNoApprovalAction = ($null -eq $observerAction.task)
  }
}

$result | ConvertTo-Json -Depth 8
