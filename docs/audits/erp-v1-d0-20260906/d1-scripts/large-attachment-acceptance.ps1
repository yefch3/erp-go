. "$PSScriptRoot/acceptance.ps1" -SeedOnly
$session=$people.S1.session
$body=@{title='D1 7MB attachment';customer='D1 attachment customer';products=@(@{product='Steel';quantity='1';unit='MT'})}
$inquiry=(Api $session POST '/inquiry-workspace' @{action='save';view='SALES';body=$body}).item
$payload=[byte[]]::new(7*1024*1024)
for($index=0;$index -lt $payload.Length;$index+=4096){$payload[$index]=[byte](($index/4096)%251)}
$attachment=(Api $session POST '/inquiry-workspace' @{action='upload';view='SALES';id=$inquiry.id;revision=$inquiry.revision;fileName='d1-7mb.bin';fileData=[Convert]::ToBase64String($payload)}).attachment
$inquiry.body.attachments=@($attachment)
$inquiry=(Api $session POST '/inquiry-workspace' @{action='save';view='SALES';id=$inquiry.id;revision=$inquiry.revision;body=$inquiry.body}).item
$download=(Api $session POST '/inquiry-workspace' @{action='download';view='SALES';id=$inquiry.id;fileKey=$attachment.key}).url
$result=Invoke-WebRequest "$base$download" -WebSession $session -UseBasicParsing
if($result.RawContentLength -ne $payload.Length){throw "Attachment length mismatch: $($result.RawContentLength)"}
"A15 7MB attachment upload/save/download passed inquiry=$($inquiry.id) bytes=$($result.RawContentLength)"
