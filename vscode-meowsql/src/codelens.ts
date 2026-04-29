import * as vscode from "vscode";

// Matches the start of a SELECT/INSERT/UPDATE/DELETE/WITH statement,
// possibly with leading whitespace or a comment line.
const SQL_START = /^\s*(SELECT|INSERT|UPDATE|DELETE|WITH)\b/i;

export class MeowSQLCodeLensProvider implements vscode.CodeLensProvider {
  private _onDidChangeCodeLenses = new vscode.EventEmitter<void>();
  readonly onDidChangeCodeLenses = this._onDidChangeCodeLenses.event;

  provideCodeLenses(document: vscode.TextDocument): vscode.CodeLens[] {
    const lenses: vscode.CodeLens[] = [];
    const text = document.getText();
    const lines = text.split("\n");

    let i = 0;
    while (i < lines.length) {
      if (SQL_START.test(lines[i])) {
        const start = i;
        // Walk forward until we hit a blank line or end of file (statement boundary).
        while (i < lines.length && lines[i].trim() !== "") {
          i++;
        }
        const end = i - 1;
        const range = new vscode.Range(start, 0, end, lines[end].length);
        const sql = lines.slice(start, end + 1).join("\n");

        lenses.push(
          new vscode.CodeLens(range, {
            title: "🐾 Optimize with MeowSQL",
            command: "meowsql.optimizeQuery",
            arguments: [sql],
          })
        );
      }
      i++;
    }
    return lenses;
  }
}
