$ErrorActionPreference = 'Stop'
$mode = $env:FAKE_MCP_MODE
$parsed = $false

function Write-JsonLine($value) {
	[Console]::Out.WriteLine(($value | ConvertTo-Json -Compress -Depth 12))
	[Console]::Out.Flush()
}

while ($null -ne ($line = [Console]::In.ReadLine())) {
	if ($mode -eq 'timeout') {
		Start-Sleep -Seconds 5
		continue
	}
	if ($mode -eq 'exit') {
		exit 23
	}
	if ($mode -eq 'malformed') {
		[Console]::Out.WriteLine('{not-json')
		[Console]::Out.Flush()
		continue
	}
	if ($mode -eq 'oversized') {
		[Console]::Out.WriteLine(('x' * 4096))
		[Console]::Out.Flush()
		continue
	}

	$request = $line | ConvertFrom-Json
	if ($null -eq $request.id) {
		continue
	}
	if ($mode -eq 'secret-error') {
		Write-JsonLine @{ jsonrpc = '2.0'; id = $request.id; error = @{ code = -32000; message = 'super-secret-token' } }
		continue
	}

	switch ($request.method) {
		'initialize' {
			Write-JsonLine @{ jsonrpc = '2.0'; id = $request.id; result = @{ protocolVersion = '2025-06-18'; capabilities = @{ tools = @{} }; serverInfo = @{ name = 'meridian-mcp'; version = '0.1.0-test' } } }
		}
		'tools/list' {
			$tools = @('dm_parse_environment', 'dm_map_info', 'dm_check_errors') | ForEach-Object { @{ name = $_; description = $_; inputSchema = @{ type = 'object' } } }
			Write-JsonLine @{ jsonrpc = '2.0'; id = $request.id; result = @{ tools = $tools } }
		}
		'tools/call' {
			switch ($request.params.name) {
				'dm_parse_environment' {
					$parsed = $true
					$text = @{ success = $true; total_types = 11; indexed_symbols = 19; state_generation = 7 } | ConvertTo-Json -Compress
					Write-JsonLine @{ jsonrpc = '2.0'; id = $request.id; result = @{ content = @(@{ type = 'text'; text = $text }) } }
				}
				'dm_map_info' {
					if (-not $parsed) { throw 'map inspection before parse' }
					$text = @{ width = 20; height = 30; z_levels = 2; state_generation = 7 } | ConvertTo-Json -Compress
					Write-JsonLine @{ jsonrpc = '2.0'; id = $request.id; result = @{ content = @(@{ type = 'text'; text = $text }) } }
				}
				'dm_check_errors' {
					if (-not $parsed) { throw 'diagnostics before parse' }
					$diagnostics = @{ count = 1; diagnostics = @(@{ severity = 'warning'; message = 'fixture' }); state_generation = 7 }
					if ($mode -like 'diagnostic-*') {
						$diagnostics.total_count = 1045
						$diagnostics.truncated = $true
						$diagnostics.summary = @{ total = 1045; by_severity = @{ error = 127; warning = 2; hint = 916 } }
						if ($mode -eq 'diagnostic-missing-total') { $diagnostics.Remove('total_count') }
						if ($mode -eq 'diagnostic-small-total') { $diagnostics.total_count = 0 }
						if ($mode -eq 'diagnostic-bad-severity') { $diagnostics.summary.by_severity.error = 128 }
					}
					$text = $diagnostics | ConvertTo-Json -Compress -Depth 8
					Write-JsonLine @{ jsonrpc = '2.0'; id = $request.id; result = @{ content = @(@{ type = 'text'; text = $text }) } }
				}
			}
		}
	}
}
