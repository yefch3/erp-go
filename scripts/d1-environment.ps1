param([ValidateSet('Prepare','Check','StartInfra')][string]$Action='Check')
$ErrorActionPreference='Stop'
$root=([IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))).TrimEnd([char[]]'\/')
$gitPointer=Get-Content -LiteralPath (Join-Path $root '.git') -Raw
if($gitPointer -notmatch '^gitdir: (.+)'){throw 'Linked D1 worktree required'}
$gitDir=$Matches[1].Trim()
if((Get-Content -LiteralPath (Join-Path $gitDir 'HEAD') -Raw).Trim() -ne 'ref: refs/heads/codex/erp-v1-d0-20260906'){throw 'D1 branch required'}
$out=Join-Path $root '.d1-environment'
New-Item -ItemType Directory -Path $out -Force | Out-Null
$file=Join-Path $out 'compose.json'
if ($Action -eq 'Prepare') {
    $emptyEnv=Join-Path $out 'empty.env'
    [IO.File]::WriteAllText($emptyEnv,'')
    $env:INTERNAL_SIGNING_KEY='d1-isolated-test-signing-key-20260906-only'
    $text=docker compose --env-file $emptyEnv -f "$root/deploy/docker-compose.infra.yml" -f "$root/deploy/docker-compose.services.yml" config --format json
    if ($LASTEXITCODE) { throw 'Cannot resolve base compose' }
    $config=($text -join "`n") | ConvertFrom-Json -AsHashtable
    $config.name='erp-d1-20260906'
    $config.networks=@{isolated=@{internal=$true};convert=@{internal=$true};ingress=@{internal=$false}}
    foreach($entry in $config.volumes.GetEnumerator()) { $entry.Value.Remove('name'); $entry.Value.Remove('external') }
    foreach($name in @($config.services.Keys)) {
        $service=$config.services[$name]
        $service.networks=if($name -eq 'gotenberg'){@{convert=@{}}}elseif($name -eq 'mail'){@{isolated=@{};convert=@{}}}else{@{isolated=@{}}}
        if($name -in @('gateway','minio')){$service.networks.ingress=@{}}
        $service.restart='no'
        $service.Remove('container_name')
        $service.ports=@()
        if($service.ContainsKey('build')) { $service.image="erp-d1-20260906/${name}:worktree" }
        if($service.ContainsKey('environment')) {
            $e=$service.environment
            foreach($key in @($e.Keys)) {
                if($key -match 'GOOGLE_|OPENAI_|EXTRA_TENANT_|MAIL_PUBLIC_BASE_URL|MAIL_CRED_KEY') { $e[$key]='' }
            }
            if($e.ContainsKey('INTERNAL_SIGNING_KEY')){$e.INTERNAL_SIGNING_KEY=$env:INTERNAL_SIGNING_KEY}
            if($e.ContainsKey('JWT_SECRET')){$e.JWT_SECRET='d1-isolated-jwt-20260906-only'}
            if($e.ContainsKey('MINIO_BUCKET')){$e.MINIO_BUCKET='erp-d1-files'}
            if($e.ContainsKey('MINIO_PUBLIC_ENDPOINT')){$e.MINIO_PUBLIC_ENDPOINT='localhost:29000'}
            if($e.ContainsKey('MAIL_PROVIDER')){$e.MAIL_PROVIDER='dev'}
            if($e.ContainsKey('ADMIN_EMAIL')){$e.ADMIN_EMAIL='admin@d1.example.test'}
            if($e.ContainsKey('COMPANY_MAIL_DOMAINS')){$e.COMPANY_MAIL_DOMAINS='d1.example.test'}
            if($e.ContainsKey('COMPANY_NAME')){$e.COMPANY_NAME='D1 isolated acceptance'}
            if($e.ContainsKey('FRONTEND_BASE_URL')){$e.FRONTEND_BASE_URL='http://localhost:25173'}
            if($e.ContainsKey('PROCUREMENT_MAIL_SENDER_ID')){$e.PROCUREMENT_MAIL_SENDER_ID='0'}
        }
    }
    $config.services.procurement.environment.EXPORT_ADDR='export:9006'
    foreach($entry in @{postgres=25433;redis=26380;minio=29000;gateway=28080}.GetEnumerator()) {
        $target=@{postgres=5432;redis=6379;minio=9000;gateway=8080}[$entry.Key]
        $config.services[$entry.Key].ports=@(@{target=$target;published=[string]$entry.Value;host_ip='127.0.0.1';protocol='tcp'})
    }
    $config.services.kafka.environment.KAFKA_ADVERTISED_LISTENERS='INTERNAL://kafka:9092,EXTERNAL://localhost:29092'
    $initFile=Join-Path $out 'init-databases.sh'
    [IO.File]::WriteAllText($initFile,((Get-Content "$root/deploy/init-databases.sh" -Raw).Replace("`r`n","`n")),[Text.UTF8Encoding]::new($false))
    foreach($v in $config.services.postgres.volumes){if($v.type -eq 'bind'){$v.source=$initFile}}
    $config | ConvertTo-Json -Depth 100 | Set-Content -LiteralPath $file -Encoding utf8
}
if(!(Test-Path $file)){throw 'Run Prepare first'}
$c=Get-Content $file -Raw | ConvertFrom-Json -AsHashtable
if($c.name -ne 'erp-d1-20260906'){throw 'Unexpected project'}
foreach($entry in $c.networks.GetEnumerator()){if($entry.Value.external -or (!$entry.Value.internal -and $entry.Key -ne 'ingress')){throw 'Unexpected network'}}
foreach($entry in $c.services.GetEnumerator()){if($entry.Value.networks.ContainsKey('ingress') -and $entry.Key -notin @('gateway','minio')){throw 'Only gateway and test files may use ingress'}}
foreach($volume in $c.volumes.Values){if($volume.external -or $volume.name){throw 'Volumes must be project scoped'}}
foreach($service in $c.services.Values){
    foreach($p in $service.ports){if($p.host_ip -ne '127.0.0.1' -or [int]$p.published -lt 25000){throw 'Unsafe published port'}}
    if($service.environment.DB_DSN -and $service.environment.DB_DSN -notmatch '@postgres:5432/erp_[a-z]+\?sslmode=disable$'){throw 'Unexpected database target'}
    if($service.build -and [IO.Path]::GetFullPath($service.build.context) -ne $root){throw 'Build is not this worktree'}
    foreach($v in $service.volumes){if($v.type -eq 'bind' -and !([IO.Path]::GetFullPath($v.source)).StartsWith($root+[IO.Path]::DirectorySeparatorChar)){throw 'Bind outside worktree'}}
}
Write-Output "Validated: $file; dedicated internal networks and volumes; loopback ports; database targets are isolated postgres only."
if($Action -eq 'StartInfra'){
    docker compose -f $file up -d postgres redis kafka minio
    if($LASTEXITCODE){throw 'D1 infrastructure startup failed'}
}
