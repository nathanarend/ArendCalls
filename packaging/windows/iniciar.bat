@echo off
setlocal
title ArendCalls
cd /d "%~dp0"

if not exist "arendcalls.exe" (
    echo ERRO: arendcalls.exe nao encontrado em %CD%
    pause
    exit /b 1
)

if not exist ".env" (
    copy /y ".env.example" ".env" >nul
    echo Arquivo .env criado. Preencha API_KEY e o login do painel,
    echo salve e rode o iniciar.bat de novo.
    notepad ".env"
    pause
    exit /b 0
)

for /f "usebackq eol=# tokens=1,* delims==" %%A in (".env") do set "%%A=%%B"
if not defined ADDR set "ADDR=127.0.0.1:8080"

if "%API_KEY%"=="troque_esta_chave" (
    echo ERRO: troque o API_KEY no arquivo .env antes de iniciar.
    pause
    exit /b 1
)

if "%CLOUDFLARE_QUICK_TUNNEL%"=="1" (
    where cloudflared >nul 2>&1
    if errorlevel 1 (
        echo AVISO: cloudflared nao encontrado no PATH, tunnel nao iniciado.
    ) else (
        start "Cloudflare Tunnel" cmd /k cloudflared tunnel --url http://localhost:%ADDR:*:=%
    )
)

echo Iniciando ArendCalls em %ADDR% ...
arendcalls.exe -addr %ADDR%

echo.
echo ArendCalls encerrou (codigo %errorlevel%).
pause
