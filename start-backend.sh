#!/bin/bash

# Start Maybe Finance Backend
echo "🚀 Starting Maybe Finance Backend..."

# Check if .env exists
if [ ! -f ".env" ]; then
    echo "❌ .env file not found!"
    echo "📝 Creating .env from .env.example..."
    cp .env.example .env
fi

# Load environment variables from .env
export $(grep -v '^#' .env | xargs)

echo "✓ Environment variables loaded"
echo "📡 PORT: $PORT"
echo "📡 ALLOWED_ORIGIN: $ALLOWED_ORIGIN"
echo "💾 DATABASE: $DATABASE_URL"

# Check if backend binary exists
if [ ! -f "maybe-backend" ]; then
    echo "❌ Backend binary not found!"
    echo "📦 Building backend..."
    go build -o maybe-backend
    if [ $? -ne 0 ]; then
        echo "❌ Build failed!"
        exit 1
    fi
    chmod +x maybe-backend
fi

# Start the backend
echo ""
echo "🎯 Starting backend on http://localhost:$PORT"
echo ""

./maybe-backend

# Made with Bob
