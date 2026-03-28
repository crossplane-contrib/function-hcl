import * as fs from 'fs';
import * as path from 'path';
import * as os from 'os';
import {ExtensionContext, window, workspace} from 'vscode';
import {LanguageClient, LanguageClientOptions, ServerOptions, TransportKind} from 'vscode-languageclient/node';
import {getBundledServerPath} from './languageServer';

let client: LanguageClient;

console.log('Extension module loaded');

export async function activate(context: ExtensionContext) {
    try {
        const serverPath = getLanguageServerPath(context);
        await startLanguageClient(context, serverPath);
    } catch (error) {
        const errorMessage = error instanceof Error ? error.message : String(error);
        window.showErrorMessage(`Failed to start function-hcl language server: ${errorMessage}`);
        console.error('Language server activation failed:', error);
        throw error;
    }
}

export function deactivate(): Thenable<void> | undefined {
    if (!client) {
        return undefined;
    }
    return client.stop();
}

async function startLanguageClient(context: ExtensionContext, serverPath: string): Promise<void> {
    const serverOptions: ServerOptions = {
        run: {
            command: serverPath,
            transport: TransportKind.stdio,
            args: ['serve'],
        },
        debug: {
            command: serverPath,
            transport: TransportKind.stdio,
            args: ['serve'],
        },
    };

    const watcher = workspace.createFileSystemWatcher('**/*.hcl');
    context.subscriptions.push(watcher);

    const outputChannel = window.createOutputChannel('function-hcl language server');
    context.subscriptions.push(outputChannel);

    const clientOptions: LanguageClientOptions = {
        documentSelector: [
            { pattern: '**/*.hcl' },
        ],
        synchronize: {
            fileEvents: watcher,
        },
        outputChannel,
    };

    client = new LanguageClient(
        'fHclLanguageServer',
        'function-hcl language server',
        serverOptions,
        clientOptions
    );

    await client.start();
}

function getLanguageServerPath(context: ExtensionContext): string {
    const config = workspace.getConfiguration('function-hcl');

    // Priority 1: User-provided path from VSCode settings
    const userPath = config.get<string>('languageServerPath');
    if (userPath && userPath.trim() !== '') {
        const resolved = resolvePath(userPath);
        if (fs.existsSync(resolved)) {
            console.log(`Using user-provided language server at: ${resolved}`);
            return resolved;
        } else {
            throw new Error(`Configured language server path does not exist: ${resolved}`);
        }
    }

    // Priority 2: Environment variable
    const envPath = process.env.FUNCTION_HCL_LS_PATH;
    if (envPath && envPath.trim() !== '') {
        const resolved = resolvePath(envPath);
        if (fs.existsSync(resolved)) {
            console.log(`Using language server from FUNCTION_HCL_LS_PATH: ${resolved}`);
            return resolved;
        } else {
            throw new Error(`FUNCTION_HCL_LS_PATH does not exist: ${resolved}`);
        }
    }

    // Priority 3: Bundled binary
    return getBundledServerPath(context);
}

function resolvePath(p: string): string {
    if (p.startsWith('~')) {
        p = path.join(os.homedir(), p.slice(1));
    }
    return path.resolve(p);
}
