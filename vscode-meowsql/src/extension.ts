import * as vscode from "vscode";
import { MeowSQLCodeLensProvider } from "./codelens";
import { runAnalyze } from "./runner";
import { ResultPanel } from "./panel";

export function activate(context: vscode.ExtensionContext) {
  const codeLens = new MeowSQLCodeLensProvider();
  context.subscriptions.push(
    vscode.languages.registerCodeLensProvider(
      [
        { language: "sql" },
        { language: "pgsql" },
        { language: "mysql" },
      ],
      codeLens
    )
  );

  context.subscriptions.push(
    vscode.commands.registerCommand(
      "meowsql.optimizeQuery",
      async (sql: string) => {
        await optimize(context, sql);
      }
    )
  );

  context.subscriptions.push(
    vscode.commands.registerCommand(
      "meowsql.optimizeSelection",
      async () => {
        const editor = vscode.window.activeTextEditor;
        if (!editor) return;
        const selection = editor.selection;
        if (selection.isEmpty) {
          vscode.window.showWarningMessage("MeowSQL: Select a SQL query first.");
          return;
        }
        const sql = editor.document.getText(selection);
        await optimize(context, sql);
      }
    )
  );
}

async function optimize(
  context: vscode.ExtensionContext,
  sql: string
): Promise<void> {
  const config = vscode.workspace.getConfiguration("meowsql");

  let dsn = config.get<string>("dsn", "").trim();
  if (!dsn) {
    const input = await vscode.window.showInputBox({
      title: "MeowSQL: Database connection string",
      prompt:
        "Enter your DSN (e.g. postgres://user:pass@localhost:5432/mydb). It will not be saved.",
      ignoreFocusOut: true,
      password: false,
      placeHolder: "postgres://user:pass@localhost:5432/mydb",
    });
    if (!input) return;
    dsn = input.trim();
  }

  const panel = ResultPanel.create(context);
  panel.showLoading(sql);

  try {
    const result = await runAnalyze({
      sql,
      dsn,
      binaryPath: config.get<string>("binaryPath", "meowsql"),
      model: config.get<string>("model", ""),
      apiKey: config.get<string>("anthropicApiKey", ""),
    });
    panel.showResult(sql, result);
  } catch (err) {
    panel.showError(err instanceof Error ? err.message : String(err));
  }
}

export function deactivate() {}
