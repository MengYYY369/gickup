import { defineConfig, devices } from "@playwright/test";

const binary = process.platform === "win32" ? "gickup-e2e.exe" : "gickup-e2e";
const runBinary = process.platform === "win32" ? `..\\${binary}` : `../${binary}`;

export default defineConfig({
  testDir: "./e2e",
  use: {
    baseURL: "http://127.0.0.1:4173",
  },
  webServer: {
    command: `npm run build && go build -o ../${binary} .. && ${runBinary} webui --port 4173 --config-dir .`,
    port: 4173,
    reuseExistingServer: !process.env.CI,
  },
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
});
