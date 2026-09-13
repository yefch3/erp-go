param([ValidateSet('Prepare','StartInfra','StartServices','CheckRuntime')][string]$Action='CheckRuntime')
$ErrorActionPreference='Stop'
function ConvertTo-Hashtable {
  param([Parameter(ValueFromPipeline=$true)]$InputObject)
  process {
    if($null -eq $InputObject){return $null}
    if($InputObject -is [System.Collections.IDictionary]){
      $result=@{};foreach($key in $InputObject.Keys){$result[$key]=ConvertTo-Hashtable $InputObject[$key]};return $result
    }
    if($InputObject -is [System.Management.Automation.PSCustomObject]){
      $result=@{};foreach($property in $InputObject.PSObject.Properties){$result[$property.Name]=ConvertTo-Hashtable $property.Value};return $result
    }
    if($InputObject -is [System.Collections.IEnumerable] -and $InputObject -isnot [string]){
      $items=@($InputObject|ForEach-Object { ConvertTo-Hashtable $_ })
      return ,$items
    }
    return $InputObject
  }
}
$root=([IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))).TrimEnd([char[]]'\/')
$gitPointer=Get-Content -LiteralPath (Join-Path $root '.git') -Raw
if($gitPointer -notmatch '^gitdir: (.+)'){throw 'Linked D6 worktree required'}
$gitDir=$Matches[1].Trim()
if((Get-Content -LiteralPath (Join-Path $gitDir 'HEAD') -Raw).Trim() -ne 'ref: refs/heads/codex/erp-v1-d6-20260911'){throw 'D6 branch required'}
$out=Join-Path $root '.d6-environment';New-Item -ItemType Directory -Path $out -Force|Out-Null
$file=Join-Path $out 'compose.json'
if($Action -eq 'Prepare'){
  $empty=Join-Path $out 'empty.env';[IO.File]::WriteAllText($empty,'')
  $env:INTERNAL_SIGNING_KEY='d6-isolated-test-signing-key-20260911-only'
  $text=docker compose --env-file $empty -f "$root/deploy/docker-compose.infra.yml" -f "$root/deploy/docker-compose.services.yml" config --format json
  if($LASTEXITCODE){throw 'Cannot resolve compose'}
  $c=ConvertTo-Hashtable (($text -join "`n")|ConvertFrom-Json);$c.name='erp-d6-20260911'
  $c.networks=@{isolated=@{internal=$true};convert=@{internal=$true};ingress=@{internal=$false}}
  foreach($entry in @($c.volumes.GetEnumerator())){$c.volumes[$entry.Key]=@{name="erp-d5-20260910_$($entry.Key)";external=$true}}
  foreach($name in @($c.services.Keys)){
    $s=$c.services[$name];$s.networks=if($name -eq 'gotenberg'){@{convert=@{}}}elseif($name -eq 'mail'){@{isolated=@{};convert=@{}}}else{@{isolated=@{}}}
    if($name -in @('gateway','minio','postgres')){$s.networks.ingress=@{}}
    $s.restart='no';$s.Remove('container_name');$s.ports=@()
    if($s.build){$s.image="erp-d6-20260911/${name}:worktree"}
    if($s.environment){$e=$s.environment;foreach($k in @($e.Keys)){if($k -match 'GOOGLE_|OPENAI_|EXTRA_TENANT_|MAIL_PUBLIC_BASE_URL|MAIL_CRED_KEY'){$e[$k]=''}};if($e.ContainsKey('INTERNAL_SIGNING_KEY')){$e.INTERNAL_SIGNING_KEY=$env:INTERNAL_SIGNING_KEY};if($e.ContainsKey('JWT_SECRET')){$e.JWT_SECRET='d6-isolated-jwt-20260911-only'};if($e.ContainsKey('MINIO_BUCKET')){$e.MINIO_BUCKET='erp-d6-files'};if($e.ContainsKey('MINIO_PUBLIC_ENDPOINT')){$e.MINIO_PUBLIC_ENDPOINT='localhost:29211'};if($e.ContainsKey('MAIL_PROVIDER')){$e.MAIL_PROVIDER='dev'};if($e.ContainsKey('ADMIN_EMAIL')){$e.ADMIN_EMAIL='admin@d5.example.test'};if($e.ContainsKey('COMPANY_MAIL_DOMAINS')){$e.COMPANY_MAIL_DOMAINS='d5.example.test'};if($e.ContainsKey('COMPANY_NAME')){$e.COMPANY_NAME='D6 acceptance on D5 verified data baseline'};if($e.ContainsKey('FRONTEND_BASE_URL')){$e.FRONTEND_BASE_URL='http://localhost:25375'};if($e.ContainsKey('PROCUREMENT_MAIL_SENDER_ID')){$e.PROCUREMENT_MAIL_SENDER_ID='0'}}
  }
  $c.services.procurement.environment.EXPORT_ADDR='export:9006'
  foreach($p in @{postgres=25644;redis=26582;minio=29211;gateway=28282}.GetEnumerator()){$target=@{postgres=5432;redis=6379;minio=9000;gateway=8080}[$p.Key];$c.services[$p.Key].ports=@(@{target=$target;published=[string]$p.Value;host_ip='127.0.0.1';protocol='tcp'})}
  $c.services.kafka.environment.KAFKA_ADVERTISED_LISTENERS='INTERNAL://kafka:9092,EXTERNAL://localhost:29294'
  $init=Join-Path $out 'init-databases.sh';[IO.File]::WriteAllText($init,((Get-Content "$root/deploy/init-databases.sh" -Raw).Replace("`r`n","`n")),[Text.UTF8Encoding]::new($false));foreach($v in $c.services.postgres.volumes){if($v.type -eq 'bind'){$v.source=$init}}
  $c|ConvertTo-Json -Depth 100|Set-Content -LiteralPath $file -Encoding utf8
  Write-Output "Prepared $file"
  exit
}
if(!(Test-Path $file)){throw 'Run Prepare first'}
$c=ConvertTo-Hashtable (Get-Content $file -Raw|ConvertFrom-Json)
if($c.name -ne 'erp-d6-20260911'){throw 'Unexpected project'}
foreach($s in $c.services.Values){foreach($p in $s.ports){if($p.host_ip -ne '127.0.0.1'){throw 'Non-loopback port'}};if($s.build -and [IO.Path]::GetFullPath($s.build.context) -ne $root){throw 'Build outside D6 worktree'}}
if($Action -eq 'StartInfra'){docker compose -f $file up -d postgres redis kafka minio;if($LASTEXITCODE){throw 'D6 infra failed'}}
if($Action -eq 'StartServices'){docker compose -f $file up -d --build;if($LASTEXITCODE){throw 'D6 services failed'}}
if($Action -eq 'CheckRuntime'){$ids=@(docker ps -aq --filter 'label=com.docker.compose.project=erp-d6-20260911');if($ids.Count -lt 15){throw 'D6 runtime incomplete'};$containers=docker inspect @ids|ConvertFrom-Json;foreach($x in $containers){if(!$x.State.Running){throw "Stopped: $($x.Name)"};foreach($n in $x.NetworkSettings.Networks.PSObject.Properties.Name){if($n -notlike 'erp-d6-20260911_*'){throw "Foreign network: $n"}}};$health=Invoke-WebRequest http://127.0.0.1:28282/api/healthz -UseBasicParsing -TimeoutSec 15;if($health.StatusCode-ne 200){throw 'Gateway unhealthy'};Write-Output "Runtime verified: $($containers.Count) D6 containers; isolated networks/volumes; gateway 200."}
