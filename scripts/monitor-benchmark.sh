#!/bin/bash
# Real-time monitoring of benchmark progress

echo "🧪 BENCHMARK MONITOR"
echo "====================="
echo ""

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

while true; do
    clear
    echo -e "${YELLOW}🧪 BENCHMARK PROGRESS${NC}"
    echo "======================="
    echo "Time: $(date '+%H:%M:%S')"
    echo ""

    # Check if log exists and show tail
    if [ -f /tmp/benchmark.log ]; then
        echo -e "${GREEN}📊 Last Output:${NC}"
        tail -15 /tmp/benchmark.log
        echo ""
    fi

    # Check running processes
    echo -e "${YELLOW}📈 Active Processes:${NC}"
    ps aux | grep -E "(benchmark|llama-server|model-loader)" | grep -v grep | awk '{print $12, $13, $14}' | head -5
    echo ""

    # Check if results files exist
    echo -e "${YELLOW}📁 Output Files:${NC}"
    if [ -f /tmp/qwen-benchmark-results.json ]; then
        SIZE=$(du -h /tmp/qwen-benchmark-results.json | cut -f1)
        echo -e "${GREEN}✅ Results JSON: $SIZE${NC}"
    else
        echo "⏳ Results JSON: not yet created"
    fi

    if [ -f /tmp/qwen-benchmark-report.txt ]; then
        SIZE=$(du -h /tmp/qwen-benchmark-report.txt | cut -f1)
        echo -e "${GREEN}✅ Report TXT: $SIZE${NC}"
    else
        echo "⏳ Report TXT: not yet created"
    fi

    echo ""
    echo "Press Ctrl+C to exit. Refreshing in 10 seconds..."
    sleep 10
done
