/**
 * YouTube video downloader using youtubei.js with Function-constructor evaluator.
 *
 * Usage:
 *   node youtube_dl.mjs --video-id <ID> --output <path>
 *   node youtube_dl.mjs --video-id <ID> --info-only
 *
 * Outputs JSON to stdout:
 *   { "status": "ok", "path": "...", "size": 12345 }
 *   { "status": "ok", "title": "...", "duration": 115, "author": "..." }
 *   { "status": "error", "message": "..." }
 */
import { Innertube, Platform } from 'youtubei.js';
import fs from 'fs';
import { parseArgs } from 'util';

// --- JS evaluator using Function constructor (no vm, no sandbox, no timeout issues) ---
Platform.shim.eval = (code, env) => {
    if (typeof code === 'object' && code.output) {
        return new Function(code.output)();
    }
    if (typeof code === 'string') {
        return new Function(code)();
    }
    throw new Error('Unknown eval format: ' + typeof code);
};

// --- CLI argument parsing ---
const { values: args } = parseArgs({
    options: {
        'video-id': { type: 'string' },
        'output': { type: 'string' },
        'info-only': { type: 'boolean', default: false },
    },
    strict: true,
});

function output(obj) {
    process.stdout.write(JSON.stringify(obj) + '\n');
}

async function main() {
    const videoId = args['video-id'];
    if (!videoId) {
        output({ status: 'error', message: 'Missing --video-id' });
        process.exit(1);
    }

    const yt = await Innertube.create({ generate_session_locally: true });

    if (args['info-only']) {
        const info = await yt.getBasicInfo(videoId);
        output({
            status: 'ok',
            title: info.basic_info.title || '',
            author: info.basic_info.author || '',
            duration: info.basic_info.duration || 0,
            thumbnail: info.basic_info.thumbnail?.[0]?.url || null,
        });
        return;
    }

    const outputPath = args['output'];
    if (!outputPath) {
        output({ status: 'error', message: 'Missing --output' });
        process.exit(1);
    }

    // Download with retry (YouTube may reset connections under rate limiting)
    const MAX_RETRIES = 3;
    for (let attempt = 1; attempt <= MAX_RETRIES; attempt++) {
        try {
            const stream = await yt.download(videoId, {
                type: 'video+audio',
                quality: 'best',
                format: 'mp4',
            });

            const ws = fs.createWriteStream(outputPath);
            let bytes = 0;
            for await (const chunk of stream) {
                ws.write(chunk);
                bytes += chunk.length;
            }
            ws.end();
            await new Promise((resolve, reject) => {
                ws.on('finish', resolve);
                ws.on('error', reject);
            });

            output({ status: 'ok', path: outputPath, size: bytes });
            return;
        } catch (err) {
            const retryable = err.message === 'terminated' || err.message === 'fetch failed';
            if (attempt < MAX_RETRIES && retryable) {
                // Wait before retry (exponential backoff)
                const delay = attempt * 5000;
                await new Promise(r => setTimeout(r, delay));
                continue;
            }
            throw err;
        }
    }
}

main().catch(err => {
    output({ status: 'error', message: err.message || String(err) });
    process.exit(1);
});
