param(
    [Parameter(Mandatory = $true)]
    [string]$SourceRoot,

    [Parameter(Mandatory = $true)]
    [string]$BuildRoot,

    [Parameter(Mandatory = $true)]
    [string]$StageRoot,

    [Parameter(Mandatory = $true)]
    [ValidateSet('x86', 'x64')]
    [string]$Platform
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$ExpectedCommit = 'efe0d1d1ea86a8a727bf26a501a261765e836766'

function Invoke-CheckedNative {
    param(
        [Parameter(Mandatory = $true)]
        [string]$FilePath,

        [Parameter(ValueFromRemainingArguments = $true)]
        [string[]]$ArgumentList
    )

    & $FilePath @ArgumentList
    if ($LASTEXITCODE -ne 0) {
        throw "$FilePath failed with exit code $LASTEXITCODE"
    }
}

function Find-DefenderCommand {
    $commands = @(
        Get-ChildItem -Path "$env:ProgramData\Microsoft\Windows Defender\Platform\*\MpCmdRun.exe" `
            -ErrorAction SilentlyContinue | Sort-Object -Property FullName -Descending
    )
    $legacy = Join-Path $env:ProgramFiles 'Windows Defender\MpCmdRun.exe'
    if (Test-Path -LiteralPath $legacy) {
        $commands += Get-Item -LiteralPath $legacy
    }
    if ($commands.Count -eq 0) {
        throw 'Microsoft Defender command-line scanner is unavailable'
    }
    return $commands[0].FullName
}

function Invoke-DefenderGate {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )

    $scanner = Find-DefenderCommand
    Write-Host "Microsoft Defender custom scan: $Path"
    & $scanner -Scan -ScanType 3 -File $Path -DisableRemediation
    $scanExit = $LASTEXITCODE
    if ($scanExit -ne 0) {
        throw "Microsoft Defender scan failed or detected a threat (exit code $scanExit)"
    }
}

$source = (Resolve-Path -LiteralPath $SourceRoot).Path
$build = [IO.Path]::GetFullPath($BuildRoot)
$stage = [IO.Path]::GetFullPath($StageRoot)

$actualCommit = (& git -C $source rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0 -or $actualCommit -ne $ExpectedCommit) {
    throw "unexpected OPC Foundation source commit: $actualCommit"
}

$trackedBinaries = @(& git -C $source ls-files -- '*.exe' '*.dll' '*.msi' '*.msix' '*.cab')
if ($LASTEXITCODE -ne 0) {
    throw 'could not inspect tracked OPC Foundation files'
}
if ($trackedBinaries.Count -ne 0) {
    throw "the pinned source unexpectedly contains tracked binaries: $($trackedBinaries -join ', ')"
}

$cmakeFiles = @(Get-ChildItem -LiteralPath $source -Recurse -File | Where-Object {
    $_.Name -eq 'CMakeLists.txt' -or $_.Extension -eq '.cmake'
} | Select-Object -ExpandProperty FullName)
if ($cmakeFiles.Count -eq 0) {
    throw 'the pinned source contains no CMake definition files'
}
$forbiddenBuildFeatures = Select-String -Path $cmakeFiles `
    -Pattern 'FetchContent|ExternalProject|execute_process|file\s*\(\s*DOWNLOAD|install\s*\(\s*(CODE|SCRIPT)' `
    -CaseSensitive:$false
if ($null -ne $forbiddenBuildFeatures) {
    throw 'the pinned direct CMake build path contains an unexpected download or execution feature'
}

if (Test-Path -LiteralPath $build) {
    throw "build directory must not already exist: $build"
}
if (Test-Path -LiteralPath $stage) {
    throw "stage directory must not already exist: $stage"
}
New-Item -ItemType Directory -Path $stage -Force | Out-Null

$generatorPlatform = if ($Platform -eq 'x86') { 'Win32' } else { 'x64' }
$arrayPatch = Join-Path $PSScriptRoot 'opcf-validation-arrays.patch'
# Apply the test item changes to the pinned checkout during the build.
Invoke-CheckedNative -FilePath 'git' -ArgumentList @('-C', $source, 'apply', '--unidiff-zero', $arrayPatch)
try {
    Invoke-DefenderGate -Path $source
    Invoke-CheckedNative -FilePath 'cmake' -ArgumentList @(
        '-S', $source,
        '-B', $build,
        '-A', $generatorPlatform,
        '-DOPC_BUILD_TESTS=ON',
        '-DOPC_ENABLE_SPECTRE=OFF'
    )
    Invoke-CheckedNative -FilePath 'cmake' -ArgumentList @(
        '--build', $build,
        '--config', 'Release',
        '--target', 'opccomn_ps', 'opcproxy', 'opcsec_ps', 'OpcTestServer'
    )
}
finally {
    Invoke-CheckedNative -FilePath 'git' -ArgumentList @('-C', $source, 'apply', '--unidiff-zero', '--reverse', $arrayPatch)
}

$serverName = "OpcTestServer_$Platform.exe"
$requiredFiles = @(
    $serverName
    'opccomn_ps.dll'
    'opcproxy.dll'
    'opcsec_ps.dll'
)
foreach ($name in $requiredFiles) {
    $matches = @(Get-ChildItem -Path $build -Filter $name -File -Recurse)
    if ($matches.Count -ne 1) {
        throw "expected exactly one $name build output, found $($matches.Count)"
    }
    Copy-Item -LiteralPath $matches[0].FullName -Destination (Join-Path $stage $name)
}

$sourceConfig = Join-Path $source 'Source\Test\TestServer\OpcTestServer.config.xml'
$stagedConfig = Join-Path $stage "OpcTestServer_$Platform.config.xml"
$configText = [IO.File]::ReadAllText($sourceConfig)
$readOnlyNeedle = '<Value xsi:type="xsd:string">OPC Test</Value>'
if ($configText.IndexOf($readOnlyNeedle, [StringComparison]::Ordinal) -lt 0 -or
    $configText.IndexOf($readOnlyNeedle, $configText.IndexOf($readOnlyNeedle) + 1, [StringComparison]::Ordinal) -ge 0) {
    throw 'the pinned test configuration no longer has exactly one expected String value marker'
}
$readOnlyProperty = '<Property PropertyID="5" xsi:type="xsd:int">1</Property>'
$configText = $configText.Replace($readOnlyNeedle, "$readOnlyNeedle`r`n          $readOnlyProperty")
$everyoneAccessNeedle = 'AllowEveryoneAccess="true"'
if ($configText.IndexOf($everyoneAccessNeedle, [StringComparison]::Ordinal) -lt 0 -or
    $configText.IndexOf($everyoneAccessNeedle, $configText.IndexOf($everyoneAccessNeedle) + 1, [StringComparison]::Ordinal) -ge 0) {
    throw 'the pinned test configuration no longer has exactly one expected everyone-access marker'
}
$configText = $configText.Replace($everyoneAccessNeedle, 'AllowEveryoneAccess="false"')
$serverClosingTag = '</Server>'
if ([regex]::Matches($configText, $serverClosingTag).Count -ne 1) {
    throw 'the pinned test configuration no longer has exactly one Server closing tag'
}
# The pinned DA 2.05a sample uses these ArrayOf types through the same cache
# implementation. Keep the original Test branch intact for the scalar probes.
$arrayFixture = @'
  <BrowseElement ElementName="ValidationArrays">
    <Children>
      <BrowseElement ElementName="Int32">
        <Item>
          <Value xsi:type="ArrayOfInt">
            <Element>2</Element>
            <Element>-7</Element>
            <Element>31</Element>
          </Value>
          <Property PropertyID="6" xsi:type="xsd:float">100</Property>
        </Item>
      </BrowseElement>
      <BrowseElement ElementName="String">
        <Item>
          <Value xsi:type="ArrayOfString">
            <Element>배열</Element>
            <Element>fixture</Element>
          </Value>
          <Property PropertyID="6" xsi:type="xsd:float">100</Property>
        </Item>
      </BrowseElement>
      <BrowseElement ElementName="Matrix">
        <Item>
          <Value xsi:type="ArrayOfInt"><Element>0</Element></Value>
          <Property PropertyID="6" xsi:type="xsd:float">100</Property>
        </Item>
      </BrowseElement>
      <BrowseElement ElementName="OffsetMatrix">
        <Item>
          <Value xsi:type="ArrayOfInt"><Element>0</Element></Value>
          <Property PropertyID="6" xsi:type="xsd:float">100</Property>
        </Item>
      </BrowseElement>
    </Children>
  </BrowseElement>
'@
# The pinned sample defines analog items with EU Type, High EU, and Low EU.
$analogFixture = @'
  <BrowseElement ElementName="ValidationAnalog">
    <Children>
      <BrowseElement ElementName="Float">
        <Item>
          <Value xsi:type="xsd:float">25</Value>
          <Property PropertyID="6" xsi:type="xsd:float">100</Property>
          <Property PropertyID="7" xsi:type="xsd:int">1</Property>
          <Property PropertyID="102" xsi:type="xsd:double">100</Property>
          <Property PropertyID="103" xsi:type="xsd:double">0</Property>
        </Item>
      </BrowseElement>
    </Children>
  </BrowseElement>
'@
$configText = $configText.Replace($serverClosingTag, "$arrayFixture`r`n$analogFixture`r`n$serverClosingTag")
[IO.File]::WriteAllText($stagedConfig, $configText, [Text.UTF8Encoding]::new($false))

Invoke-DefenderGate -Path $stage

Write-Host "Built audited OPC Foundation DA 2.05a test server from $ExpectedCommit"
foreach ($file in Get-ChildItem -Path $stage -File | Sort-Object -Property Name) {
    $hash = (Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash
    Write-Host "SHA256 $($file.Name) $hash"
}
