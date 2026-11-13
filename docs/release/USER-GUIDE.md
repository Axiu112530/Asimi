# Use Asimi

Asimi is a local-first AI desktop assistant for everyday questions, writing, image analysis and help with tasks on your computer.

Standard mode is the default. Choose **Settings → General → Interface mode → Developer** to open workspaces, workflows, the model playground, and advanced settings.

## Connect your own provider

On the first launch, choose a language. English is the default. Choose Kimi, GLM, DeepSeek, Qwen, Tencent Hy, OpenAI, Claude, or Gemini, enter your API key, then select **Connect and save**. The test sends one short request to the provider and may use account credit. A successful test saves the key using Windows DPAPI and sets the default model. Select **Set up later** to browse projects and history. Open AI providers to connect a model when you want to chat. In Developer mode, **AI providers → Advanced provider settings** supports compatible Chat Completions and Responses endpoints.

The Kimi, GLM, Qwen, and Tencent Hy presets use the China-region APIs. The Qwen preset uses the shared Beijing DashScope endpoint. For a workspace-specific Qwen key, use the endpoint shown in your Model Studio console in the advanced editor. The Claude preset accepts a workspace-scoped Anthropic API key. OpenAI, Claude, and Gemini require a supported region and an API account with model access. Asimi connects directly from your computer.

Use **Chat settings** to switch models. When a model accepts text only, Asimi blocks image attachments and screenshot analysis before sending data. A conversation that already includes images requires an image-capable model to continue with that history. For video, attach still frames.

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

Use the dedicated test window to exercise computer actions. Check the target and exact action before approving, then read the verification result. For screenshot analysis, choose the display or region, inspect the preview, cover any sensitive content and separately confirm the upload to an image-capable provider. Real desktop visual analysis and empty Notepad remain user-initiated acceptance scenarios. [Known limitations](KNOWN-LIMITATIONS.md)
