#Requires -RunAsAdministrator
# wdac-allow-mod.ps1
# Creates a WDAC Supplemental Policy that allows loading any DLL from
# D:\Stardew Valley\Mods\StardewMCPBridge\ without a code signature.
# Uses a path rule so re-builds don't require re-running this script.

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$ModDir       = "D:\Stardew Valley\Mods\StardewMCPBridge"
$BasePolicyId = "{0283AC0F-FFF1-49AE-ADA1-8A933130CAD6}"
$PolicyName   = "SmartNPC-ModAllow"
$WorkDir      = "$env:TEMP\SmartNPC-WDAC"
$XmlPath      = "$WorkDir\$PolicyName.xml"
$CipPath      = "$WorkDir\$PolicyName.cip"
$ActiveDir    = "C:\Windows\System32\CodeIntegrity\CiPolicies\Active"

Write-Host ""
Write-Host "== SmartNPC WDAC Supplemental Policy Installer ==" -ForegroundColor Cyan
Write-Host ""

if (-not (Test-Path $ModDir)) {
    Write-Host "ERROR: Mod directory not found: $ModDir" -ForegroundColor Red
    Write-Host "       Run 'task mod:install' first, then re-run this script." -ForegroundColor Yellow
    exit 1
}
Write-Host "OK  Mod directory : $ModDir" -ForegroundColor Green

$BasePolicyFile = "$ActiveDir\$BasePolicyId.cip"
if (-not (Test-Path $BasePolicyFile)) {
    Write-Host "ERROR: Base policy file not found: $BasePolicyFile" -ForegroundColor Red
    exit 1
}
Write-Host "OK  Base policy   : $BasePolicyFile" -ForegroundColor Green

New-Item -ItemType Directory -Force -Path $WorkDir | Out-Null
Write-Host "OK  Work dir      : $WorkDir" -ForegroundColor Green
Write-Host ""

# Step 1: create path rule
Write-Host "[1/4] Creating path rule..." -ForegroundColor Yellow
$Rules = New-CIPolicyRule -FilePathRule "$ModDir\*"

# Step 2: generate supplemental policy XML
Write-Host "[2/4] Generating supplemental policy XML..." -ForegroundColor Yellow
New-CIPolicy -Rules $Rules -FilePath $XmlPath

# Patch XML: set PolicyType=Supplemental and add BasePolicyId setting
$Xml = [xml](Get-Content $XmlPath -Encoding UTF8)
$ns  = "urn:schemas-microsoft-com:sipolicy"
$nsm = New-Object System.Xml.XmlNamespaceManager($Xml.NameTable)
$nsm.AddNamespace("si", $ns)

$policyNode = $Xml.SelectSingleNode("//si:SiPolicy", $nsm)
if ($policyNode.HasAttribute("PolicyType")) {
    $policyNode.SetAttribute("PolicyType", "Supplemental Policy")
}

$settingsNode = $Xml.SelectSingleNode("//si:Settings", $nsm)
if (-not $settingsNode) {
    $settingsNode = $Xml.CreateElement("Settings", $ns)
    $policyNode.AppendChild($settingsNode) | Out-Null
}

$existing = $settingsNode.SelectSingleNode(
    "si:Setting[@Provider='PolicyInfo' and @Key='Information' and @ValueName='BasePolicyId']", $nsm)
if (-not $existing) {
    $setting = $Xml.CreateElement("Setting", $ns)
    $setting.SetAttribute("Provider", "PolicyInfo")
    $setting.SetAttribute("Key", "Information")
    $setting.SetAttribute("ValueName", "BasePolicyId")
    $value = $Xml.CreateElement("Value", $ns)
    $str   = $Xml.CreateElement("String", $ns)
    $str.InnerText = $BasePolicyId
    $value.AppendChild($str)  | Out-Null
    $setting.AppendChild($value) | Out-Null
    $settingsNode.AppendChild($setting) | Out-Null
}
$Xml.Save($XmlPath)
Write-Host "    Saved: $XmlPath" -ForegroundColor Gray

# Step 3: convert to binary .cip
Write-Host "[3/4] Converting to binary .cip..." -ForegroundColor Yellow
ConvertFrom-CIPolicy -XmlFilePath $XmlPath -BinaryFilePath $CipPath
Write-Host "    Generated: $CipPath" -ForegroundColor Gray

# Step 4: deploy to Active directory
Write-Host "[4/4] Deploying to $ActiveDir ..." -ForegroundColor Yellow
$FinalXml   = [xml](Get-Content $XmlPath -Encoding UTF8)
$nsm2       = New-Object System.Xml.XmlNamespaceManager($FinalXml.NameTable)
$nsm2.AddNamespace("si", "urn:schemas-microsoft-com:sipolicy")
$guidNode   = $FinalXml.SelectSingleNode("//si:PolicyTypeID", $nsm2)
$NewGuid    = if ($guidNode) { $guidNode.InnerText } else { "{$(New-Guid)}" }
$DestCip    = "$ActiveDir\$NewGuid.cip"
Copy-Item -Force -Path $CipPath -Destination $DestCip
Write-Host "    Deployed : $DestCip" -ForegroundColor Gray

# Refresh without reboot
Write-Host ""
Write-Host "Refreshing CI policy (CiTool.exe --refresh)..." -ForegroundColor Yellow
try {
    $out = & CiTool.exe --refresh 2>&1
    Write-Host $out
    Write-Host "OK  Policy refreshed. No reboot needed." -ForegroundColor Green
} catch {
    Write-Host "WARN: CiTool --refresh failed. A reboot may be required." -ForegroundColor Yellow
    Write-Host "      $_" -ForegroundColor Gray
}

Write-Host ""
Write-Host "== Done ==" -ForegroundColor Cyan
Write-Host "   Policy ID   : $NewGuid" -ForegroundColor Cyan
Write-Host "   Path allowed: $ModDir\*" -ForegroundColor Cyan
Write-Host "   Run run-wsl.bat to start the game." -ForegroundColor Cyan
Write-Host ""
