// Official WeCom scan-to-create. The platform publishes this as a
// browser popup (`@wecom/wecom-aibot-sdk` → openBotInfoAuthWindow),
// not a Device-Flow QR URL like Feishu. We load the SDK only on click
// so Next's static export never evaluates `window` at build time.
//
// Docs: https://www.npmjs.com/package/@wecom/wecom-aibot-sdk

export type WeComScanBot = {
  botid: string;
  secret: string;
};

const WECOM_SCAN_SOURCE = "fastclaw";

export async function openOfficialWeComScan(): Promise<WeComScanBot> {
  const mod = await import("@wecom/wecom-aibot-sdk");
  const sdk = (mod as { default?: WeComScanSDK }).default ?? (mod as unknown as WeComScanSDK);
  if (!sdk || typeof sdk.openBotInfoAuthWindow !== "function") {
    throw new Error("Official WeCom scan SDK failed to load");
  }
  const bot = await sdk.openBotInfoAuthWindow({ source: WECOM_SCAN_SOURCE });
  if (!bot?.botid || !bot?.secret) {
    throw new Error("WeCom scan returned empty botid/secret");
  }
  return bot;
}

type WeComScanSDK = {
  openBotInfoAuthWindow: (options: {
    source: string;
    debug?: boolean;
  }) => Promise<WeComScanBot>;
};
