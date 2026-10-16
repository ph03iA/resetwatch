"use strict";

const platforms = {
  windows: {
    download: "resetwatch_0.1.0_windows_amd64.zip",
    label: "Windows x64",
    shell: "POWERSHELL",
    help: "In PowerShell, open the folder where you downloaded the archive.",
    executable: ".\\resetwatch.exe",
    unpack: "Expand-Archive .\\resetwatch_0.1.0_windows_amd64.zip -DestinationPath .\\resetwatch\ncd .\\resetwatch\n.\\resetwatch.exe version",
  },
  macos: {
    download: "resetwatch_0.1.0_darwin_arm64.tar.gz",
    label: "macOS Apple silicon",
    shell: "ZSH / BASH",
    help: "In Terminal, open the folder where you downloaded the archive. This build is for Apple silicon Macs.",
    executable: "./resetwatch",
    unpack: "tar -xzf resetwatch_0.1.0_darwin_arm64.tar.gz\nchmod +x resetwatch\n./resetwatch version",
  },
  linux: {
    download: "resetwatch_0.1.0_linux_amd64.tar.gz",
    label: "Linux x64",
    shell: "BASH",
    help: "In your terminal, open the folder where you downloaded the archive. This build is for x64 Linux.",
    executable: "./resetwatch",
    unpack: "tar -xzf resetwatch_0.1.0_linux_amd64.tar.gz\nchmod +x resetwatch\n./resetwatch version",
  },
};

const tabs = [...document.querySelectorAll("[data-os]")];
const status = document.getElementById("copy-status");

function updateInstallCommands(platform) {
  const notify = `${platform.executable} notify --email you@example.com --from 'ResetWatch <onboarding@resend.dev>'`;
  document.getElementById("install-command").textContent = platform.unpack;
  document.getElementById("connect-command").textContent = `${platform.executable} connect codex`;
  document.getElementById("watch-command").textContent = `${notify}\n${platform.executable} watch`;
}

function updateDownload(platform) {
  const download = document.getElementById("platform-download");
  download.href = `./downloads/${platform.download}`;
  download.firstChild.textContent = `${platform.label} `;
  document.getElementById("shell-label").textContent = platform.shell;
  document.getElementById("unpack-help").textContent = platform.help;
}

function selectPlatform(tab) {
  const platform = platforms[tab.dataset.os];
  tabs.forEach((item) => {
    const selected = item === tab;
    item.setAttribute("aria-selected", String(selected));
    item.tabIndex = selected ? 0 : -1;
  });
  document.getElementById("installation-panel").setAttribute("aria-labelledby", tab.id);
  updateDownload(platform);
  updateInstallCommands(platform);
  status.textContent = "";
}

function handleTabKey(event) {
  const offsets = { ArrowRight: 1, ArrowLeft: -1, Home: -tabs.indexOf(event.currentTarget), End: tabs.length - 1 - tabs.indexOf(event.currentTarget) };
  const offset = offsets[event.key];
  if (offset === undefined) return;
  event.preventDefault();
  const nextIndex = (tabs.indexOf(event.currentTarget) + offset + tabs.length) % tabs.length;
  selectPlatform(tabs[nextIndex]);
  tabs[nextIndex].focus();
}

function selectCommandText(target) {
  const selection = window.getSelection();
  if (!selection) return;
  const range = document.createRange();
  range.selectNodeContents(target);
  selection.removeAllRanges();
  selection.addRange(range);
}

function showCopySuccess(button) {
  button.firstChild.textContent = "Copied ";
  button.dataset.copied = "true";
  status.textContent = "Commands copied to your clipboard.";
  window.setTimeout(() => {
    button.firstChild.textContent = "Copy ";
    delete button.dataset.copied;
  }, 2200);
}

async function copyCommand(event) {
  const button = event.currentTarget;
  const target = document.getElementById(button.dataset.copy);
  try {
    await navigator.clipboard.writeText(target.textContent);
    showCopySuccess(button);
  } catch {
    selectCommandText(target);
    status.textContent = "Clipboard access is unavailable. The commands are selected; use your keyboard to copy.";
  }
}

tabs.forEach((tab) => {
  tab.addEventListener("click", () => selectPlatform(tab));
  tab.addEventListener("keydown", handleTabKey);
});
document.querySelectorAll("[data-copy]").forEach((button) => button.addEventListener("click", copyCommand));
