param([ValidateSet('Prepare','StartInfra','StartServices','CheckRuntime')][string]$Action='CheckRuntime')
$ErrorActionPreference='Stop'
$root=([IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))).TrimEnd([char[]]'\/')
$gitPointer=Get-Content -LiteralPath (Join-Path $root '.git') -Raw
if($gitPointer -notmatch '^gitdir: (.+)'){throw 'Linked D5 worktree required'}
$gitDir=$Matches[1].Trim()
if((Get-Content -LiteralPath (Join-Path $gitDir 'HEAD') -Raw).Trim() -ne 'ref: refs/heads/codex/erp-v1-d5-20260910'){throw 'D5 branch required'}
$out=Join-Path $root '.d5-environment';New-Item -ItemType Directory -Path $out -Force|Out-Null
$file=Join-Path $out 'compose.json'
if($Action -eq 'Prepare'){
  $empty=Join-Path $out 'empty.env';[IO.File]::WriteAllText($empty,'')
  $env:INTERNAL_SIGNING_KEY='d5-isolated-test-signing-key-20260910-only'
  $text=docker compose --env-file $empty -f "$root/deploy/docker-compose.infra.yml" -f "$root/deploy/docker-compose.services.yml" config --format json
  if($LASTEXITCODE){throw 'Cannot resolve compose'}
  $c=($text -join "`n")|ConvertFrom-Json -AsHashtable;$c.name='erp-d5-20260910'
  $c.networks=@{isolated=@{internal=$true};convert=@{internal=$true};ingress=@{internal=$false}}
  foreach($v in $c.volumes.Values){$v.Remove('name');$v.Remove('external')}
  foreach($name in @($c.services.Keys)){
    $s=$c.services[$name];$s.networks=if($name -eq 'gotenberg'){@{convert=@{}}}elseif($name -eq 'mail'){@{isolated=@{};convert=@{}}}else{@{isolated=@{}}}
    if($name -in @('gateway','minio','postgres')){$s.networks.ingress=@{}}
    $s.restart='no';$s.Remove('container_name');$s.ports=@()
    if($s.build){$s.image="erp-d5-20260910/${name}:worktree"}
    if($s.environment){$e=$s.environment;foreach($k in @($e.Keys)){if($k -match 'GOOGLE_|OPENAI_|EXTRA_TENANT_|MAIL_PUBLIC_BASE_URL|MAIL_CRED_KEY'){$e[$k]=''}};if($e.ContainsKey('INTERNAL_SIGNING_KEY')){$e.INTERNAL_SIGNING_KEY=$env:INTERNAL_SIGNING_KEY};if($e.ContainsKey('JWT_SECRET')){$e.JWT_SECRET='d5-isolated-jwt-20260910-only'};if($e.ContainsKey('MINIO_BUCKET')){$e.MINIO_BUCKET='erp-d5-files'};if($e.ContainsKey('MINIO_PUBLIC_ENDPOINT')){$e.MINIO_PUBLIC_ENDPOINT='localhost:29211'};if($e.ContainsKey('MAIL_PROVIDER')){$e.MAIL_PROVIDER='dev'};if($e.ContainsKey('ADMIN_EMAIL')){$e.ADMIN_EMAIL='admin@d5.example.test'};if($e.ContainsKey('COMPANY_MAIL_DOMAINS')){$e.COMPANY_MAIL_DOMAINS='d5.example.test'};if($e.ContainsKey('COMPANY_NAME')){$e.COMPANY_NAME='D5 isolated acceptance'};if($e.ContainsKey('FRONTEND_BASE_URL')){$e.FRONTEND_BASE_URL='http://localhost:25375'};if($e.ContainsKey('PROCUREMENT_MAIL_SENDER_ID')){$e.PROCUREMENT_MAIL_SENDER_ID='0'}}
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
$c=Get-Content $file -Raw|ConvertFrom-Json -AsHashtable
if($c.name -ne 'erp-d5-20260910'){throw 'Unexpected project'}
foreach($s in $c.services.Values){foreach($p in $s.ports){if($p.host_ip -ne '127.0.0.1'){throw 'Non-loopback port'}};if($s.build -and [IO.Path]::GetFullPath($s.build.context) -ne $root){throw 'Build outside D5 worktree'}}
if($Action -eq 'StartInfra'){docker compose -f $file up -d postgres redis kafka minio;if($LASTEXITCODE){throw 'D5 infra failed'}}
if($Action -eq 'StartServices'){docker compose -f $file up -d --build;if($LASTEXITCODE){throw 'D5 services failed'}}
if($Action -eq 'CheckRuntime'){$ids=@(docker ps -aq --filter 'label=com.docker.compose.project=erp-d5-20260910');if($ids.Count -lt 15){throw 'D5 runtime incomplete'};$containers=docker inspect @ids|ConvertFrom-Json;foreach($x in $containers){if(!$x.State.Running){throw "Stopped: $($x.Name)"};foreach($n in $x.NetworkSettings.Networks.PSObject.Properties.Name){if($n -notlike 'erp-d5-20260910_*'){throw "Foreign network: $n"}}};$health=Invoke-WebRequest http://127.0.0.1:28282/api/healthz -TimeoutSec 15;if($health.StatusCode-ne 200){throw 'Gateway unhealthy'};Write-Output "Runtime verified: $($containers.Count) D5 containers; isolated networks/volumes; gateway 200."}
