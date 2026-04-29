import { spawn } from "child_process";
import * as os from "os";
import * as fs from "fs";
import * as path from "path";

export interface AnalyzeOptions {
  sql: string;
  dsn: string;
  binaryPath: string;
  model: string;
  apiKey: string;
}

export interface AnalyzeResult {
  diagnosis: string;
  root_causes: string[];
  index_ddl: string;
  rewritten_query: string;
  estimated_improvement: string;
  warnings: string[];
}

export function runAnalyze(opts: AnalyzeOptions): Promise<AnalyzeResult> {
  return new Promise((resolve, reject) => {
    // Write SQL to a temp file to avoid shell quoting issues.
    const tmpFile = path.join(os.tmpdir(), `meowsql-${Date.now()}.sql`);
    fs.writeFileSync(tmpFile, opts.sql, "utf8");

    const args = [
      "analyze",
      "--dsn", opts.dsn,
      "--file", tmpFile,
      "--json",
    ];
    if (opts.model) {
      args.push("--model", opts.model);
    }

    const env: NodeJS.ProcessEnv = { ...process.env };
    if (opts.apiKey) {
      env["ANTHROPIC_API_KEY"] = opts.apiKey;
    }

    const child = spawn(opts.binaryPath, args, { env });

    let stdout = "";
    let stderr = "";

    child.stdout.on("data", (d: Buffer) => { stdout += d.toString(); });
    child.stderr.on("data", (d: Buffer) => { stderr += d.toString(); });

    child.on("error", (err) => {
      fs.unlinkSync(tmpFile);
      if ((err as NodeJS.ErrnoException).code === "ENOENT") {
        reject(new Error(
          `meowsql binary not found at "${opts.binaryPath}".\n` +
          `Install it with: brew install YoungsoonLee/meowsql/meowsql\n` +
          `Or set meowsql.binaryPath in VS Code settings.`
        ));
      } else {
        reject(err);
      }
    });

    child.on("close", (code) => {
      try { fs.unlinkSync(tmpFile); } catch {}

      if (code !== 0) {
        reject(new Error(stderr || `meowsql exited with code ${code}`));
        return;
      }

      try {
        // meowsql --json output: { "result": { ... } }
        const outer = JSON.parse(stdout);
        const result: AnalyzeResult = outer.result ?? outer;
        resolve(result);
      } catch {
        reject(new Error(`Failed to parse meowsql output:\n${stdout}`));
      }
    });
  });
}
