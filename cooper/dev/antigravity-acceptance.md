# Antigravity account acceptance

Run these checks on the physical Linux host after the automated gates pass.
The development VM cannot prove the host's browser, shell startup, or clipboard
behavior. Use test accounts. Enter credentials and authorization codes only
in Google's browser page and the native agy prompt.

## Private setup

Use a separate home, workspace, config, image prefix, and runtime namespace.
A separate home alone does not isolate an OS keyring. The generated host
wrapper selects file authentication before any login. Do not run the original
native executable directly with this private home.

From the repository root on the physical host:

```sh
cooper_accept_binary="$PWD/cooper/cooper"
cooper_accept_home=$(mktemp -d "$HOME/cooper-antigravity-acceptance.XXXXXX")
mkdir "$cooper_accept_home/workspace"
cooper_accept_env() {
    env -u GEMINI_API_KEY -u GOOGLE_GEMINI_BASE_URL -u AGY_ADC_AUTH \
        -u GOOGLE_APPLICATION_CREDENTIALS -u CLOUDSDK_CONFIG \
        -u GOOGLE_CLOUD_PROJECT -u GOOGLE_CLOUD_LOCATION \
        -u CLOUD_CODE_URL -u BAICODE_ENDPOINT_URL -u JETSKI_OAUTH_TOKEN \
        HOME="$cooper_accept_home" ZDOTDIR="$cooper_accept_home" \
        PATH="$cooper_accept_home/.local/share/cooper/antigravity/bin:$PATH" "$@"
}
cooper_accept() {
    cooper_accept_env env SHELL=/bin/false "$cooper_accept_binary" \
        --config "$cooper_accept_home/.cooper" \
        --prefix agy-acceptance- --runtime-namespace agy-acceptance "$@"
}
cooper_accept_agy() {
    cooper_accept_env "$cooper_accept_home/.local/share/cooper/antigravity/bin/agy" "$@"
}
cooper_accept configure
```

The Cooper command disables login-shell credential lookup. Native agy keeps
the host shell setting. Both use a new home with no credential cache and
retain the physical desktop bus. The agy
wrapper must select file authentication even while that bus remains available.
`ZDOTDIR` keeps shell setup inside the private home too.

Select Antigravity 1.2.2 in Pin mode and disable other AI tools. Select unused
proxy and bridge ports if another Cooper instance is active. Native host agy
must already be installed. Build with the same private home:

```sh
cooper_accept build
test -x "$cooper_accept_home/.local/share/cooper/antigravity/bin/agy"
cd "$cooper_accept_home/workspace"
cooper_accept_agy
```

## Host login and fresh shell

1. Complete Google OAuth through the wrapped host command. Check browser
   opening and the manual URL/code path if the browser cannot open. Do not
   log in inside a Cooper runtime.
2. Exit agy normally. Check only file presence, without printing credentials:

   ```sh
   test -s "$cooper_accept_home/.gemini/antigravity-cli/antigravity-oauth-token"
   cooper_accept save antigravity
   ```

   The first profile must be `Default`. This save must work while the physical
   desktop bus is still available.
3. Start a fresh interactive Bash process. Enter ordinary `agy` in that shell,
   with no D-Bus prefix. Check that it restores the same account, then exit:

   ```sh
   cooper_accept_env bash --noprofile --rcfile "$cooper_accept_home/.bashrc" -i
   ```

   `command -v agy` must select the private Cooper wrapper. Repeat with Zsh if
   it is the user's normal shell. Automated tests also start a shell whose
   initial PATH does not contain the wrapper.
4. Repeat `cooper_accept build`. Existing shell content must remain intact,
   and a fresh wrapped host launch must still restore the file account.

## Host, Docker, and VM conversation

Start the control panel in one terminal:

```sh
cooper_accept up
```

Keep it open. Use the same variable values and functions in another terminal;
do not create a second temporary home.

1. Start `cooper_accept_agy` on the host. Ask for a short answer with a unique
   harmless marker. Record the conversation ID and exit agy.
2. Run `cooper_accept cli antigravity`, then `agy --conversation=<id>`. Confirm
   the same account and marker. Add a second marker, exit agy, and leave the
   Cooper shell.
3. Run `cooper_accept vm antigravity --cpus 4 --memory 4096m --disk 24g`,
   then restore that conversation. Confirm
   both markers and make a small workspace file change through a native tool.
   Check its content, then exit agy and the Cooper shell.
4. Restore the conversation through `cooper_accept_agy` on the host. Confirm
   the VM turn and file change. Check text/image paste in each execution mode.
   Record only outcomes, versions, and required hostnames.
5. Repeat after natural token expiry to check refresh and restoration by a
   fresh process. Do not print or change token contents to accelerate this
   check. Exit each writer before starting the next one.

The default policy must allow the core flow without added approvals. If a
host is denied, record the operation and exact hostname. Add a justified
managed default or document the optional feature; do not use allow-all to pass.
Use these same VM resource overrides for the named-profile checks. They match
the passed development tests and limit memory use while the Codex VM is active.

## Profiles and refreshed state

Close host Google state writers and stop related Docker/VM runtimes before
save/load. A named session changes the saved profile's files, not the active
host account. Loading that profile restores its updated state to the host.

1. Save Default again. Run `cooper_accept load antigravity Work` to create a
   fresh account state. Sign in to Work with `cooper_accept_agy`, exit it, and
   run `cooper_accept save antigravity` without a destination name.
2. Load Default and confirm its account and conversation on the host. Exit agy.
3. Start a named Work Docker session and VM session in turn. Both must use
   Work while the active host files remain Default. Record a new conversation
   in Work. After a runtime refresh, stop the related runtimes.
4. Load Work on the host and confirm its account, updated conversation, and
   refreshed login. Return to Default and confirm its account is preserved.
5. Check restart and normal cache cleanup. Saved profiles and the host wrapper
   must remain usable. Unknown-account refusal and recovery are also covered
   by automated tests; do not force a save when identity cannot be verified.

The desktop bus must remain running throughout these checks. File profiles
are supported because Cooper verifies its selected host wrapper. Clearing
`DBUS_SESSION_BUS_ADDRESS` alone is not the setup procedure.

Keep the private home until results are recorded and the user chooses to
remove its test credentials. `cooper_accept down` stops the isolated runtime.
The report must separate host login, fresh-shell restoration, conversation
continuity, real token refresh, profile switching, and clipboard results.
macOS Keychain, ADC, and WIF profiles remain outside this file-OAuth check.
