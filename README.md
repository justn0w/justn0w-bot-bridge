#### 一、项目实现能力介绍
* 打通飞书机器人 claude code
* 实现值班答疑的能力

#### 二、快速开始

1. 配置凭证

   凭证不入代码库，通过 `.env` 注入。从模板复制后填入真实值：

   ```bash
   cp .env.example .env
   ```

   | 变量 | 说明 |
   | --- | --- |
   | `FEISHU_APP_ID` | 飞书应用 App ID |
   | `FEISHU_APP_SECRET` | 飞书应用 App Secret |

   凭证获取：飞书开放平台 → 开发者后台 → 凭证与基础信息。

   > `.env` 已被 `.gitignore` 忽略。生产环境可直接注入同名环境变量，
   > 已有环境变量优先级高于 `.env`，无需该文件。

   **另一项前置条件（无对应环境变量）：** 答疑由本机的 `claude` 命令完成，
   因此运行服务的机器必须已登录 Claude Code（在本机执行一次 `claude` 走完登录流程，
   登录态由 CLI 自己保存）。未登录时飞书侧连接正常，但每次提问都会返回兜底话术。

   可执行文件不在 `PATH` 里时，改 `configs/config.yaml` 的 `claude.cli_path`
   填绝对路径；单次调用超时同理，见该段的 `timeout_sec`。

2. 启动服务

   ```bash
   make run        # 等价于 go run ./cmd/server
   ```

   凭证缺失时启动会立即失败并提示缺少的变量名，不会静默连上飞书。

3. 可选：用 `ENV_FILE` 指定其他 dotenv 文件路径（多环境部署）

   ```bash
   ENV_FILE=.env.staging go run ./cmd/server
   ```

