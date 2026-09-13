# Antigravity account acceptance

Run this check on the physical host after the automated gates pass. The VM
used for development cannot prove access to the physical host's keyring or
clipboard. Use a test account. Enter credentials and the authorization code
only in Google's browser page and the native `agy` prompt.

## Private setup

Use a separate home, workspace, config, image prefix, and runtime namespace.
A separate home does not isolate an OS keyring. Start the first login inside
Cooper, which does not receive the host session bus or keyring. Do not run a
host `agy` with this private home while a host keyring can supply another
account.

From the repository root on the physical host:

```sh
cooper_accept_binary="$PWD/cooper/cooper"
cooper_accept_home=$(mktemp -d "$HOME/cooper-antigravity-acceptance.XXXXXX")
mkdir "$cooper_accept_home/workspace"
cooper_accept() {
    env -u GEMINI_API_KEY -u GOOGLE_GEMINI_BASE_URL -u AGY_ADC_AUTH \
        -u GOOGLE_APPLICATION_CREDENTIALS -u CLOUDSDK_CONFIG \
        -u GOOGLE_CLOUD_PROJECT -u GOOGLE_CLOUD_LOCATION \
        -u CLOUD_CODE_URL -u BAICODE_ENDPOINT_URL -u JETSKI_OAUTH_TOKEN \
        HOME="$cooper_accept_home" "$cooper_accept_binary" \
        --config "$cooper_accept_home/.cooper" \
        --prefix agy-acceptance- --runtime-namespace agy-acceptance "$@"
}
cooper_accept configure
```

Select Antigravity 1.2.2 in Pin mode and disable other AI tools. Use unused
proxy and bridge ports if another Cooper instance is active. Build with this
same private home, then start the control panel:

```sh
cooper_accept build
cd "$cooper_accept_home/workspace"
cooper_accept up
```

Keep that terminal open. In another terminal, use the same variable values
and function. Do not create a second temporary home.

## Real account and conversation

1. Run `cooper_accept cli antigravity`, then `agy`. Select Google OAuth. The
   native screen offers a browser URL and a place to paste the returned code.
   This flow passed with a real consumer file-OAuth account in the private
   development fixture. Complete the login yourself for host acceptance.
2. Ask for a short answer with a unique harmless marker. Record the native
   conversation ID. Exit `agy` normally, then leave the Cooper shell.
3. Run `cooper_accept vm antigravity`, then `agy --conversation=<id>`. Ask it to
   recall the marker. Make one small file change through a native tool, check
   its content, and exit normally.
4. Return to the Docker barrel and restore the same conversation. Check the
   VM turn and file change. Check text/image paste through the native UI in
   each mode. Record only outcomes, versions, and required hostnames.
5. Repeat after native token expiry to check refresh. Do not change token
   contents or print token files to accelerate this test. Exit each writer
   before switching runtime or account.

The default policy must allow the core flow without added approvals. If a
host is denied, record the operation and exact hostname. Add a justified
managed default or document it as optional; do not use allow-all to pass.

## Host continuity and profiles

Host continuity needs a separate check with the same whole `.gemini` root,
workspace, and native version. It is supported only when the host actually
uses portable file state or the reviewed Gemini API-key mode. A desktop
keyring account is an open compatibility limit, not a passed test.

For a supported file account, exit all Google state writers, save Default,
create Work with `cooper_accept load antigravity Work`, and log in to Work.
Save without a destination name. Return to Default and confirm its account
and conversation. Start a named Work Docker session and VM session; confirm
that both use Work while the host remains on Default. Check outgoing state
preservation, unknown-account refusal, restart, and cleanup. Do not force a
save if Cooper cannot identify the current account.

Keep the private home until results are recorded and the user chooses to
remove its test credentials. `cooper_accept down` stops this isolated runtime.
The final report must separate passed Docker/VM file-state behavior, actual
host continuity, token refresh, and unsupported keyring/ADC/WIF profiles.
