# Slack 操作

只有用户明确请求 Slack 操作时才读取本 reference。写入前确认已安装命令和目标位置：

    hotplex slack --help
    hotplex slack <subcommand> --help

支持的命令族包括发送或更新消息、上传或下载文件、书签、表情回复、频道和定时消息。
选择最窄的子命令，保留用户指定的频道或消息标识。目标缺失时应询问，不要自行推断。

只读示例：

    hotplex slack list-channels --json

明确授权的变更示例：

    hotplex slack send-message \
      --channel "<CHANNEL_ID>" \
      --text "Requested status update"

消息发送、上传、更新、删除、表情回复和定时消息都属于外部副作用。执行前说明目标和
副作用，并要求用户明确请求。只读频道或书签检查足够时，不要执行写入。报告中脱敏
token 和私人响应元数据。
