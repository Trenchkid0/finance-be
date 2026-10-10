#!/usr/bin/env bash
# ==============================================================================
# Script Reload Otomatis di Server
# Cara pakai: ./reload.sh
# ==============================================================================

set -e

echo "📦 1. Mengambil update terbaru dari Git..."
git pull

echo "🔑 2. Memberikan hak eksekusi ke binary..."
chmod +x ./racks-backend

echo "🚀 3. Memuat ulang (reload/restart) service..."
if command -v systemctl >/dev/null 2>&1 && systemctl is-active --quiet racks-backend; then
    sudo systemctl restart racks-backend
    echo "✅ [SELESAI] Systemd service 'racks-backend' berhasil di-restart!"
elif command -v pm2 >/dev/null 2>&1 && (pm2 list | grep -q "backend-monet" || pm2 list | grep -q "racks-backend"); then
    if pm2 list | grep -q "backend-monet"; then
        pm2 restart backend-monet
        echo "✅ [SELESAI] PM2 process 'backend-monet' berhasil di-restart!"
    else
        pm2 restart racks-backend
        echo "✅ [SELESAI] PM2 process 'racks-backend' berhasil di-restart!"
    fi
elif command -v supervisorctl >/dev/null 2>&1 && supervisorctl status racks-backend >/dev/null 2>&1; then
    sudo supervisorctl restart racks-backend
    echo "✅ [SELESAI] Supervisor service 'racks-backend' berhasil di-restart!"
else
    echo "ℹ️ Binary siap digunakan. Jika dijalankan manual di background:"
    echo "   pkill -f racks-backend || true"
    echo "   nohup ./racks-backend > backend.log 2>&1 &"
    echo "✅ Selesai!"
fi
