#!/bin/bash

# Locust Load Testing - Setup Verification Script

echo "========================================"
echo "Locust Load Testing - Setup Verification"
echo "========================================"
echo ""

# Check Docker
echo "1. Checking Docker..."
if command -v docker &> /dev/null; then
    echo "   ✅ Docker is installed"
    docker --version
else
    echo "   ❌ Docker is NOT installed"
    echo "   Please install Docker Desktop from https://www.docker.com/products/docker-desktop"
    exit 1
fi

# Check Docker Compose
echo ""
echo "2. Checking Docker Compose..."
if docker compose version &> /dev/null; then
    echo "   ✅ Docker Compose is available"
    docker compose version
elif command -v docker-compose &> /dev/null; then
    echo "   ✅ Docker Compose is available (standalone)"
    docker-compose --version
else
    echo "   ❌ Docker Compose is NOT available"
    exit 1
fi

# Check if Docker daemon is running
echo ""
echo "3. Checking if Docker daemon is running..."
if docker info &> /dev/null; then
    echo "   ✅ Docker daemon is running"
else
    echo "   ❌ Docker daemon is NOT running"
    echo "   Please start Docker Desktop"
    exit 1
fi

# Check required files
echo ""
echo "4. Checking required files..."
required_files=(
    "main.go"
    "locustfile.py"
    "docker-compose.yml"
    "Dockerfile.server"
    "go.mod"
)

all_files_present=true
for file in "${required_files[@]}"; do
    if [ -f "$file" ]; then
        echo "   ✅ $file"
    else
        echo "   ❌ $file is missing"
        all_files_present=false
    fi
done

if [ "$all_files_present" = false ]; then
    echo ""
    echo "   Missing files detected. Please ensure all files are in the current directory."
    exit 1
fi

# Clean up any previous containers
echo ""
echo "5. Cleaning up any previous containers..."
docker-compose down 2>/dev/null
echo "   ✅ Cleanup complete"

echo ""
echo "========================================"
echo "Setup verification complete!"
echo "========================================"
echo ""
echo "Next steps:"
echo "1. Run: docker-compose up"
echo "2. Wait for all containers to start (may take 1-2 minutes on first run)"
echo "3. Open browser to: http://localhost:8089"
echo "4. Configure test parameters and start swarming!"
echo ""
echo "If you encounter 'go mod download' errors, this is normal on first build."
echo "The go.sum file will be generated automatically during the build."
echo ""
echo "To monitor progress: docker-compose logs -f"
echo "To stop: Press Ctrl+C, then run: docker-compose down"
echo ""
