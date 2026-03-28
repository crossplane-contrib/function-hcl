import { existsSync, mkdirSync, chmodSync, unlinkSync, createWriteStream } from 'fs';
import { pipeline } from 'stream/promises';
import { join, dirname } from 'path';
import { fileURLToPath } from 'url';
import { x as extract } from 'tar';

const __dirname = dirname(fileURLToPath(import.meta.url));

function log(msg) {
    process.stderr.write(msg + '\n');
}

const BINARY_NAME = 'function-hcl-ls';
const GITHUB_REPO = 'crossplane-contrib/function-hcl';

// VS Code target → Go os/arch mapping
const TARGET_MAP = {
    'darwin-arm64':  { os: 'darwin',  arch: 'arm64' },
    'darwin-x64':    { os: 'darwin',  arch: 'amd64' },
    'linux-arm64':   { os: 'linux',   arch: 'arm64' },
    'linux-x64':     { os: 'linux',   arch: 'amd64' },
    'win32-x64':     { os: 'windows', arch: 'amd64' },
};

// Node.js process values → Go os/arch mapping (for local dev)
const PLATFORM_MAP = { darwin: 'darwin', linux: 'linux', win32: 'windows' };
const ARCH_MAP = { x64: 'amd64', arm64: 'arm64' };

function parseArgs() {
    const args = process.argv.slice(2);
    const result = {};
    for (let i = 0; i < args.length; i++) {
        if (args[i] === '--target' && args[i + 1]) {
            result.target = args[++i];
        } else if (args[i] === '--local-tarball' && args[i + 1]) {
            result.localTarball = args[++i];
        }
    }
    return result;
}

function resolveOsArch(target) {
    if (target) {
        const mapped = TARGET_MAP[target];
        if (!mapped) {
            throw new Error(`Unsupported target: ${target}. Supported: ${Object.keys(TARGET_MAP).join(', ')}`);
        }
        return mapped;
    }
    const os = PLATFORM_MAP[process.platform];
    const arch = ARCH_MAP[process.arch];
    if (!os || !arch) {
        throw new Error(`Unsupported platform: ${process.platform}/${process.arch}`);
    }
    return { os, arch };
}

async function getLatestVersion() {
    const response = await fetch(`https://api.github.com/repos/${GITHUB_REPO}/releases/latest`);
    if (!response.ok) {
        throw new Error(`Failed to fetch latest release: HTTP ${response.status}`);
    }
    const release = await response.json();
    return release.tag_name.replace(/^v/, '');
}

async function main() {
    const { target, localTarball } = parseArgs();
    const { os, arch } = resolveOsArch(target);

    const binaryFile = os === 'windows' ? `${BINARY_NAME}.exe` : BINARY_NAME;
    const binDir = join(__dirname, '..', 'bin');
    const binaryPath = join(binDir, binaryFile);

    if (existsSync(binaryPath)) {
        log(`Language server already exists at ${binaryPath}, skipping.`);
        return;
    }

    mkdirSync(binDir, { recursive: true });

    const assetName = `${BINARY_NAME}-${os}-${arch}.tar.gz`;

    if (localTarball) {
        // CI path: extract from a local tarball (already downloaded as workflow artifact)
        log(`Extracting ${binaryFile} from local tarball: ${localTarball}`);
        await extract({
            file: localTarball,
            cwd: binDir,
            filter: (entryPath) => entryPath.endsWith(binaryFile),
        });
    } else {
        // Local dev path: download latest release from GitHub
        const version = await getLatestVersion();
        const url = `https://github.com/${GITHUB_REPO}/releases/download/v${version}/${assetName}`;
        log(`Downloading ${binaryFile} v${version} for ${os}/${arch}...`);

        const tarballPath = join(binDir, assetName);
        try {
            await downloadFile(url, tarballPath);
            await extract({
                file: tarballPath,
                cwd: binDir,
                filter: (entryPath) => entryPath.endsWith(binaryFile),
            });
        } finally {
            if (existsSync(tarballPath)) {
                unlinkSync(tarballPath);
            }
        }
    }

    if (!existsSync(binaryPath)) {
        throw new Error(`Binary '${binaryFile}' not found in archive`);
    }

    if (os !== 'windows') {
        chmodSync(binaryPath, 0o755);
    }
    log(`Language server ready at ${binaryPath}`);
}

async function downloadFile(url, targetPath) {
    const response = await fetch(url);
    if (!response.ok) {
        throw new Error(`HTTP ${response.status}: ${response.statusText}`);
    }
    const fileStream = createWriteStream(targetPath);
    await pipeline(response.body, fileStream);
}

main().catch((err) => {
    console.error(`Failed to download language server: ${err.message}`);
    process.exit(1);
});
