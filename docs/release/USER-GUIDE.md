# Use Asimi

Asimi is a local-first AI desktop assistant for everyday questions, writing, image analysis and help with tasks on your computer.

Standard mode is the default. Choose **Settings → General → Interface mode → Developer** to open workspaces, workflows, the model playground, and advanced settings.

## Connect your own provider

On the first launch, choose a language. English is the default. Choose Kimi, GLM, DeepSeek, Qwen, Tencent Hy, OpenAI, Claude, or Gemini, select a model, enter your API key, then select **Connect and save**. Choose **Enter another model…** to use a model ID available to your account; the provider's address, protocol, and saved key are reused. The test sends one short request to the provider and may use account credit. A successful test saves the key using Windows DPAPI and sets the default model. Select **Set up later** to browse projects and history. Open AI providers to connect a model when you want to chat. For a different service, use **AI providers → Add a service** in either interface mode. Enter its name, service address, model ID, and API key. The simple form uses Chat Completions by default; a full address ending in `/responses` selects Responses automatically. Developer mode also offers **Advanced provider settings** for detailed configuration.

The Kimi, GLM, Qwen, and Tencent Hy presets use the China-region APIs. The Qwen preset uses the shared Beijing DashScope endpoint. For a workspace-specific Qwen key, add a service using the endpoint shown in your Model Studio console. The Claude preset accepts a workspace-scoped Anthropic API key. OpenAI, Claude, and Gemini require a supported region and an API account with model access. Asimi connects directly from your computer.

Use **Chat settings** to switch saved models. Custom model IDs start with unknown image capabilities; a successful text connection test does not enable image support. When a model accepts text only, Asimi blocks image attachments and screenshot analysis before sending data. A conversation that already includes images requires an image-capable model to continue with that history. For video, attach still frames.

## Find conversations

A conversation gets a short title from its first question when you send it. Existing conversations with empty titles are named from their first saved question when Asimi starts. This happens locally without an extra model request.

## Review usage and costs

Choose a time range to see requests, input and output tokens, total tokens, and estimated cost. Asimi groups usage by provider and model, keeps currencies separate, and flags missing values. Three days includes today and the previous two calendar days. The week starts on Monday in your local time zone.

Developer mode can expand today’s latest 25 requests for diagnostics. The Model Playground shows the metrics for the request you just ran. The Usage page reads from local records and keeps conversation history separate.

## Work on a project

1. Choose a local workspace. The Developer page shows the project, available runtimes and Git status.
2. In Agent Tasks, describe the change and inspect the suggested files. Confirm the context preview before sending source or test output to your provider.
3. Review the exact file diff and approve the action. Existing local changes appear in the review. Run the proposed test through a separate approval and review its command, target and network scope.
4. Read the file verification and test result before accepting the Agent summary. If the app reports an unknown result, check the actual target and prepare a new approval only if needed.

Use **Stop all** to cancel active tasks. The task history records proposed actions, approvals, execution results and recovery status.

## Review computer actions

The Computer page shows screen access, computer-action status, and supported devices. **Inspect and act** is available in Standard mode. Select a supported window, inspect its controls, then use **Set value as a task** or **Invoke as a task** to prepare a desktop task. Review the exact target and action before approving. You can check its outcome in the computer action history; Developer mode also shows its steps and evidence in Agent Tasks. A desktop task does not require choosing a code project or configuring a Skill.

Pause or resume computer actions on this page; every new action requires separate confirmation. Camera and microphone access are not available in this version. Developer mode adds capture-backend diagnostics.

During an approved computer action, press **Esc** on the Windows interactive desktop to stop the current action and pause further computer actions. The in-app activity bar also has a **Stop action** button. Stopping does not undo an action already applied; read its verification result before continuing.

Use the dedicated test window under **Inspect and act** to exercise the currently supported computer actions. For screenshot analysis, choose the display or region, inspect the preview, cover any sensitive content and separately confirm the upload to an image-capable provider. A reviewed model suggestion can prepare a desktop task; executing it requires a separate action approval. An unknown result requires a fresh observation and proposal. Real-provider vision has been verified on the dedicated fixture. Empty Notepad remains a user-initiated acceptance scenario. [Known limitations](KNOWN-LIMITATIONS.md)

The [Perception & Action implementation report](../reports/2026-11-14-perception-action-completion.zh-CN.md) identifies the tested local build. Existing release archives are separate artifacts.

## Goals and connections

Open **Goals** to start or review a chat, project, desktop, or connector task. A public GitHub repository URL can prepare an anonymous metadata read. Confirm its exact action before sending the request. Open **Connections** to configure an account token and grant operations on specific resources. The action review shows the origin, method, path, and request data. If a connector result can be verified, **Goals** can preview that result for a separately confirmed model summary.

The evolution panel shows observed skill use and outcomes, pending improvements, and memory maintenance. To create a personal composition, review its pinned skills and tool permissions, then explicitly enable it.

See the [implementation and acceptance report](../reports/2026-11-14-unified-agent-completion.zh-CN.md) for the tested Windows build and service coverage.
