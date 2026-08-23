import { describe, expect, it } from "vitest";
import { parseHelpCommandText } from "./help-command";

const helpText = `📖 *命令帮助*

*🔧 会话控制*
• \`/gc\` \`/park\` — 休眠会话（停止 Worker，保留会话）
• \`/cd\` \`<目录>\` — 切换工作目录（创建新会话）

*⚙️ 配置*
• \`/model\` \`<名称>\` — 切换 AI 模型
• \`?\` — 显示此帮助
`;

describe("parseHelpCommandText", () => {
  it("parses command groups, aliases, arguments, and descriptions", () => {
    expect(parseHelpCommandText(helpText)).toEqual({
      title: "命令帮助",
      sections: [
        {
          emoji: "🔧",
          title: "会话控制",
          entries: [
            {
              commands: ["/gc", "/park"],
              args: undefined,
              description: "休眠会话（停止 Worker，保留会话）",
            },
            {
              commands: ["/cd"],
              args: "<目录>",
              description: "切换工作目录（创建新会话）",
            },
          ],
        },
        {
          emoji: "⚙️",
          title: "配置",
          entries: [
            {
              commands: ["/model"],
              args: "<名称>",
              description: "切换 AI 模型",
            },
            {
              commands: ["?"],
              args: undefined,
              description: "显示此帮助",
            },
          ],
        },
      ],
    });
  });

  it("ignores ordinary Markdown and incomplete help content", () => {
    expect(parseHelpCommandText("这是一条普通的助手消息。")).toBeNull();
    expect(parseHelpCommandText("📖 *命令帮助*\n\n这不是完整的帮助分组")).toBeNull();
  });
});
