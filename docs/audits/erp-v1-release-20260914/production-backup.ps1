param(
  [string]$Profile = 'erp',
  [string]$Region = 'us-west-2',
  [switch]$CreateSnapshot,
  [string]$SnapshotId = ('erp-pg-pre-rebuild-' + (Get-Date -Format 'yyyyMMdd-HHmmss'))
)
$ErrorActionPreference = 'Stop'
$expectedAccount = '564623062079'
$database = 'erp-pg'
function Invoke-AwsJson([string[]]$Arguments) {
  $result = & aws --profile $Profile --region $Region --no-cli-pager @Arguments --output json
  if ($LASTEXITCODE -ne 0) { throw 'AWS command failed; no further action performed.' }
  return ($result -join "`n") | ConvertFrom-Json
}
$identity = Invoke-AwsJson @('sts','get-caller-identity')
if ($identity.Account -ne $expectedAccount) {
  throw "Wrong AWS account: expected $expectedAccount; refusing production operations."
}
$response = Invoke-AwsJson @('rds','describe-db-instances','--db-instance-identifier',$database)
$db = @($response.DBInstances)[0]
if ($db.DBInstanceStatus -ne 'available') { throw 'Production database is not available; stop and investigate.' }
[pscustomobject]@{
  Account = $identity.Account
  Database = $db.DBInstanceIdentifier
  Region = $Region
  Status = $db.DBInstanceStatus
  Encrypted = $db.StorageEncrypted
  BackupRetentionDays = $db.BackupRetentionPeriod
  LatestRestorableTime = $db.LatestRestorableTime
}
if (-not $CreateSnapshot) { return }
if ($SnapshotId -notmatch '^erp-pg-pre-rebuild-[a-z0-9-]+$') { throw 'Unexpected snapshot name.' }
$null = Invoke-AwsJson @('rds','create-db-snapshot','--db-instance-identifier',$database,'--db-snapshot-identifier',$SnapshotId)
Write-Output "Snapshot requested: $SnapshotId"
$deadline = (Get-Date).AddMinutes(30)
do {
  Start-Sleep -Seconds 20
  $response = Invoke-AwsJson @('rds','describe-db-snapshots','--db-snapshot-identifier',$SnapshotId)
  $snapshot = @($response.DBSnapshots)[0]
  if ($snapshot.Status -eq 'available') {
    [pscustomobject]@{Snapshot=$SnapshotId;Database=$database;Status=$snapshot.Status;Created=$snapshot.SnapshotCreateTime;Encrypted=$snapshot.Encrypted}
    return
  }
  if ($snapshot.Status -in @('failed','deleted','deleting')) { throw "Snapshot failed: $($snapshot.Status)" }
} while ((Get-Date) -lt $deadline)
throw "Snapshot still pending: $SnapshotId. Check this snapshot before creating another."
