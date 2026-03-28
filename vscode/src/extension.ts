import * as fs from 'fs';
import {ExtensionContext, window, workspace} from 'vscode';
import {LanguageClient, LanguageClientOptions, ServerOptions, TransportKind} from 'vscode-languageclient/node';
import {getBundledServerPath} from './languageServer';

let client: LanguageClient;

console.log('Extension module loaded');

export async function activate(context: ExtensionContext) {
    try {
        const serverPath = getLanguageServerPath(context);
        await startLanguageClient(serverPath);
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

async function startLanguageClient(serverPath: string): Promise<void> {
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

    const clientOptions: LanguageClientOptions = {
        documentSelector: [
            { pattern: '**/*.hcl' },
        ],
        synchronize: {
            fileEvents: workspace.createFileSystemWatcher('**/*.hcl'),
        },
        outputChannel: window.createOutputChannel('function-hcl language server'),
    };

    client = new LanguageClient(
        'fHclLanguageServer',
        'function-hcl language server',
        serverOptions,
        clientOptions
    );

    client.outputChannel.show();
    await client.start();
}

function getLanguageServerPath(context: ExtensionContext): string {
    const config = workspace.getConfiguration('function-hcl');

    // Priority 1: User-provided path from VSCode settings
    const userPath = config.get<string>('languageServerPath');
    if (userPath && userPath.trim() !== '') {
        if (fs.existsSync(userPath)) {
            console.log(`Using user-provided language server at: ${userPath}`);
            return userPath;
        } else {
            throw new Error(`Configured language server path does not exist: ${userPath}`);
        }
    }

    // Priority 2: Environment variable
    const envPath = process.env.FUNCTION_HCL_LS_PATH;
    if (envPath && envPath.trim() !== '') {
        if (fs.existsSync(envPath)) {
            console.log(`Using language server from FUNCTION_HCL_LS_PATH: ${envPath}`);
            return envPath;
        } else {
            throw new Error(`FUNCTION_HCL_LS_PATH does not exist: ${envPath}`);
        }
    }

    // Priority 3: Bundled binary
    return getBundledServerPath(context);
}
