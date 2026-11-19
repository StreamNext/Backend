<#
.SYNOPSIS
  Opens inbound TCP 5433 so other machines on the LAN can reach the StreamNext dev Postgres container.

.DESCRIPTION
  Run once on the Windows DB host, in an elevated (Administrator) PowerShell, from the Backend folder:
    powershell -ExecutionPolicy Bypass -File .\scripts\open-db-firewall.ps1
  The rule only applies on Private networks and only to addresses on the local subnet.
  Safe to re-run. Remove later with:
    Remove-NetFirewallRule -DisplayName "StreamNext Postgres"
#>
param([int]$Port = 5433)

$ErrorActionPreference = 'Stop'
$name = 'StreamNext Postgres'

$isAdmin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $isAdmin) { throw 'Run this in an elevated (Administrator) PowerShell.' }

if (Get-NetFirewallRule -DisplayName $name -ErrorAction SilentlyContinue) {
    Write-Host "Rule '$name' already exists; nothing to do."
    return
}

New-NetFirewallRule -DisplayName $name -Direction Inbound -Protocol TCP -LocalPort $Port `
    -Action Allow -Profile Private -RemoteAddress LocalSubnet | Out-Null
Write-Host "Added inbound rule '$name' (TCP $Port, Private networks, local subnet only)."
