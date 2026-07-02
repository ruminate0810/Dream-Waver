#!/bin/bash
# Social Trend Scanner - Dependency Installer
set -e

echo "Installing Python dependencies..."
pip3 install playwright pytrends 2>/dev/null || pip install playwright pytrends

echo "Installing Firefox browser for Playwright..."
playwright install firefox 2>/dev/null || python3 -m playwright install firefox

echo ""
echo "Setup complete."
echo ""
echo "Usage:"
echo "  As Claude Code skill:  /social-trend-scanner AI agents --industry=tech"
echo "  As CLI:                python3 -m social_trend_scanner.scripts scan 'AI agents' --industry=tech"
echo "  As Python module:      from social_trend_scanner.scripts import scan"
echo ""
echo "Industries: tech, finance, crypto, marketing, general"
echo "Platforms:  YouTube, HN, GitHub, Reddit, Google News, Trends, The Verge,"
echo "            Ars Technica, Product Hunt, Yahoo Finance, Seeking Alpha,"
echo "            CoinTelegraph, X (login), LinkedIn (login)"
