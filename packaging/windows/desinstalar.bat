@echo off
rem Para e remove o servico do ArendCalls (pede administrador sozinho).
rem Os arquivos da pasta (.env, wacalls.db, gravacoes, logs) sao mantidos.
cd /d "%~dp0"
start "" "%~dp0ArendCalls.exe" uninstall
