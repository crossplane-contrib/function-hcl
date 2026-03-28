import * as fs from 'fs';
import * as path from 'path';
import { ExtensionContext } from 'vscode';

const BINARY_NAME = process.platform === 'win32' ? 'function-hcl-ls.exe' : 'function-hcl-ls';

/**
 * Get the path to the bundled language server binary.
 */
export function getBundledServerPath(context: ExtensionContext): string {
    const binaryPath = path.join(context.extensionPath, 'bin', BINARY_NAME);
    if (!fs.existsSync(binaryPath)) {
        throw new Error(
            `Bundled language server not found at ${binaryPath}. ` +
            `The extension package may be corrupted — try reinstalling.`
        );
    }
    return binaryPath;
}
