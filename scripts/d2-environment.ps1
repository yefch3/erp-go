param([ValidateSet('Prepare','Check','StartInfra','CheckRuntime')][string]$Action='Check')
$ErrorActionPreference='Stop'
$root=([IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))).TrimEnd([char[]]'\/')
$gitPointer=Get-Content -LiteralPath (Join-Path $root '.git') -Raw
if($gitPointer -notmatch '^gitdir: (.+)'){throw 'Linked D2 worktree required'}
$gitDir=$Matches[1].Trim()
if((Get-Content -LiteralPath (Join-Path $gitDir 'HEAD') -Raw).Trim() -ne 'ref: refs/heads/codex/erp-v1-d2-20260907'){throw 'D2 branch required'}
$out=Join-Path $root '.d2-environment'
New-Item -ItemType Directory -Path $out -Force | Out-Null
$file=Join-Path $out 'compose.json'
if ($Action -eq 'Prepare') {
    $emptyEnv=Join-Path $out 'empty.env'
    [IO.File]::WriteAllText($emptyEnv,'')
    $env:INTERNAL_SIGNING_KEY='d2-isolated-test-signing-key-20260907-only'
    $text=docker compose --env-file $emptyEnv -f "$root/deploy/docker-compose.infra.yml" -f "$root/deploy/docker-compose.services.yml" config --format json
    if ($LASTEXITCODE) { throw 'Cannot resolve base compose' }
    $config=($text -join "`n") | ConvertFrom-Json -AsHashtable
    $config.name='erp-d2-20260907'
    $config.networks=@{isolated=@{internal=$true};convert=@{internal=$true};ingress=@{internal=$false}}
    foreach($entry in $config.volumes.GetEnumerator()) { $entry.Value.Remove('name'); $entry.Value.Remove('external') }
    foreach($name in @($config.services.Keys)) {
        $service=$config.services[$name]
        $service.networks=if($name -eq 'gotenberg'){@{convert=@{}}}elseif($name -eq 'mail'){@{isolated=@{};convert=@{}}}else{@{isolated=@{}}}
        if($name -in @('gateway','minio')){$service.networks.ingress=@{}}
        $service.restart='no'
        $service.Remove('container_name')
        $service.ports=@()
        if($service.ContainsKey('build')) { $service.image="erp-d2-20260907/${name}:worktree" }
        if($service.ContainsKey('environment')) {
            $e=$service.environment
            foreach($key in @($e.Keys)) {
                if($key -match 'GOOGLE_|OPENAI_|EXTRA_TENANT_|MAIL_PUBLIC_BASE_URL|MAIL_CRED_KEY') { $e[$key]='' }
            }
            if($e.ContainsKey('INTERNAL_SIGNING_KEY')){$e.INTERNAL_SIGNING_KEY=$env:INTERNAL_SIGNING_KEY}
            if($e.ContainsKey('JWT_SECRET')){$e.JWT_SECRET='d2-isolated-jwt-20260907-only'}
            if($e.ContainsKey('MINIO_BUCKET')){$e.MINIO_BUCKET='erp-d2-files'}
            if($e.ContainsKey('MINIO_PUBLIC_ENDPOINT')){$e.MINIO_PUBLIC_ENDPOINT='localhost:29001'}
            if($e.ContainsKey('MAIL_PROVIDER')){$e.MAIL_PROVIDER='dev'}
            if($e.ContainsKey('ADMIN_EMAIL')){$e.ADMIN_EMAIL='admin@d2.example.test'}
            if($e.ContainsKey('COMPANY_MAIL_DOMAINS')){$e.COMPANY_MAIL_DOMAINS='d2.example.test'}
            if($e.ContainsKey('COMPANY_NAME')){$e.COMPANY_NAME='D2 isolated acceptance'}
            if($e.ContainsKey('FRONTEND_BASE_URL')){$e.FRONTEND_BASE_URL='http://localhost:25174'}
            if($e.ContainsKey('PROCUREMENT_MAIL_SENDER_ID')){$e.PROCUREMENT_MAIL_SENDER_ID='0'}
        }
    }
    $config.services.procurement.environment.EXPORT_ADDR='export:9006'
    foreach($entry in @{postgres=25434;redis=26381;minio=29001;gateway=28081}.GetEnumerator()) {
        $target=@{postgres=5432;redis=6379;minio=9000;gateway=8080}[$entry.Key]
        $config.services[$entry.Key].ports=@(@{target=$target;published=[string]$entry.Value;host_ip='127.0.0.1';protocol='tcp'})
    }
    $config.services.kafka.environment.KAFKA_ADVERTISED_LISTENERS='INTERNAL://kafka:9092,EXTERNAL://localhost:29093'
    $initFile=Join-Path $out 'init-databases.sh'
    [IO.File]::WriteAllText($initFile,((Get-Content "$root/deploy/init-databases.sh" -Raw).Replace("`r`n","`n")),[Text.UTF8Encoding]::new($false))
    foreach($v in $config.services.postgres.volumes){if($v.type -eq 'bind'){$v.source=$initFile}}
    $config | ConvertTo-Json -Depth 100 | Set-Content -LiteralPath $file -Encoding utf8
}
if(!(Test-Path $file)){throw 'Run Prepare first'}
$c=Get-Content $file -Raw | ConvertFrom-Json -AsHashtable
if($c.name -ne 'erp-d2-20260907'){throw 'Unexpected project'}
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
    if($LASTEXITCODE){throw 'D2 infrastructure startup failed'}
}
if($Action -eq 'CheckRuntime'){
    $project='erp-d2-20260907'
    $ids=@(docker ps -aq --filter "label=com.docker.compose.project=$project")
    if($LASTEXITCODE -or $ids.Count -lt 15){throw 'D2 runtime is incomplete'}
    $containers=(docker inspect @ids | ConvertFrom-Json)
    if($LASTEXITCODE){throw 'Cannot inspect D2 containers'}
    $evidence=@()
    foreach($container in $containers){
        if(!$container.State.Running){throw "Stopped D2 container: $($container.Name)"}
        $service=$container.Config.Labels.'com.docker.compose.service'
        $networks=@($container.NetworkSettings.Networks.PSObject.Properties.Name)
        foreach($network in $networks){
            if($network -notin @("${project}_isolated","${project}_convert","${project}_ingress")){throw 'Container joined a non-D2 network'}
            if($network -eq "${project}_ingress" -and $service -notin @('gateway','minio')){throw 'Unexpected ingress access'}
        }
        foreach($mount in $container.Mounts){
            if($mount.Type -eq 'volume'){
                $owners=@(docker ps -aq --filter "volume=$($mount.Name)")
                if($LASTEXITCODE){throw 'Cannot verify volume owners'}
                foreach($owner in $owners){if($owner -notin $ids){throw 'Data volume is attached outside D2'}}
                if(!$mount.Name.StartsWith("${project}_") -and !($service -eq 'kafka' -and $mount.Destination -eq '/etc/kafka/secrets' -and $mount.Name -match '^[a-f0-9]{64}$')){throw 'Unexpected volume'}
            }
        }
        $db=@($container.Config.Env | Where-Object {$_ -like 'DB_DSN=*'})
        foreach($dsn in $db){if($dsn -notmatch '@postgres:5432/erp_[a-z]+\?sslmode=disable$'){throw 'Runtime database target is not isolated postgres'}}
        $evidence += [ordered]@{service=$service;container=$container.Name;image=$container.Config.Image;running=$container.State.Running;networks=$networks;volumes=@($container.Mounts | Where-Object Type -eq 'volume' | ForEach-Object Name);databaseTarget=if($db.Count){'postgres:5432 (D2 network)'}else{$null}}
    }
    $net=(docker network inspect "${project}_isolated" | ConvertFrom-Json)
    if($LASTEXITCODE -or !$net.Internal){throw 'D2 database network is not internal'}
    $health=Invoke-WebRequest -Uri 'http://127.0.0.1:28081/api/healthz' -TimeoutSec 15
    if($health.StatusCode -ne 200){throw 'D2 gateway health failed'}
    [ordered]@{checkedAt=[DateTimeOffset]::Now.ToString('o');project=$project;gatewayHealth=200;containers=$evidence} | ConvertTo-Json -Depth 10 | Set-Content -LiteralPath (Join-Path $out 'runtime-verification.json') -Encoding utf8
    Write-Output "Runtime verified: $($containers.Count) containers; D2-only networks and volumes; isolated database targets; gateway health 200."
}
