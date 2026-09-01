declare module "@wecom/wecom-aibot-sdk" {
  export interface BotInfo {
    botid: string;
    secret: string;
  }

  const WecomAIBotSDK: {
    openBotInfoAuthWindow(options: {
      source: string;
      debug?: boolean;
      onCreated?: (bot: BotInfo) => void;
      onError?: (error: { message: string }) => void;
    }): Promise<BotInfo>;
    closeWindow(): void;
  };

  export default WecomAIBotSDK;
}
