# Use Asimi

Asimi is a local-first AI desktop assistant for developers, combining AI conversations, project understanding, code changes, controlled computer interaction, and secure local execution.

## Connect your own provider

On the first launch, choose a language. English is the default. Select the DeepSeek preset, enter your API key, then choose **Test connection**. The test sends one short request to the provider and may use account credit. A successful test saves the key using Windows DPAPI and sets the default model. You can continue offline and configure a provider later in Settings → AI providers. The advanced editor supports compatible Chat Completions and Responses endpoints.

## Work on a project

1. Choose a local workspace. The Developer page shows the project, available runtimes and Git status.
2. In Agent Tasks, describe the change and inspect the suggested files. Confirm the context preview before sending source or test output to your provider.
3. Review the exact file diff and approve the action. Existing local changes appear in the review. Run the proposed test through a separate approval and review its command, target and network scope.
4. Read the file verification and test result before accepting the Agent summary. If the app reports an unknown result, check the actual target and prepare a new approval only if needed.

Use **Stop all** to cancel active tasks. The task history records proposed actions, approvals, execution results and recovery status.

## Review computer actions

Use the dedicated test window to exercise computer actions. Check the target and exact action before approving, then read the verification result. For screenshot analysis, choose the display or region, inspect the preview, cover any sensitive content and separately confirm the upload to an image-capable provider. Real desktop visual analysis and empty Notepad remain user-initiated acceptance scenarios. [Known limitations](KNOWN-LIMITATIONS.md)
