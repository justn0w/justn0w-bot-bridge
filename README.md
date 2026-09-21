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
   | `DEEPSEEK_API_KEY` | DeepSeek API Key（答疑模型） |

   凭证获取：飞书开放平台 → 开发者后台 → 凭证与基础信息；DeepSeek API Key 见
   <https://platform.deepseek.com/api_keys>。

   > 答疑模型默认走 DeepSeek 的 Anthropic 兼容接口。若要换成其他兼容
   > Anthropic Messages 协议的服务，改 `configs/config.yaml` 的
   > `llm.base_url` 与 `llm.model` 即可，代码无需改动。
   > 此前按 Anthropic 配置的部署也可继续用 `ANTHROPIC_API_KEY`，
   > 两者同时存在时以 `DEEPSEEK_API_KEY` 为准。

   > `.env` 已被 `.gitignore` 忽略。生产环境可直接注入同名环境变量，
   > 已有环境变量优先级高于 `.env`，无需该文件。

2. 启动服务

   ```bash
   make run        # 等价于 go run ./cmd/server
   ```

   凭证缺失时启动会立即失败并提示缺少的变量名，不会静默连上飞书。

3. 可选：用 `ENV_FILE` 指定其他 dotenv 文件路径（多环境部署）

   ```bash
   ENV_FILE=.env.staging go run ./cmd/server
   ```

