"use strict";

const clone = "git clone https://github.com/ph03iA/resetwatch.git\ncd resetwatch\n";
const unixBuild = `${clone}go build -o resetwatch ./cmd/resetwatch\n./resetwatch version`;

const platforms = {
  windows: {
    shell: "POWERSHELL",
    help: "In PowerShell, open the folder that should hold ResetWatch. Requires Git and Go 1.27.2 or newer.",
    executable: ".\\resetwatch.exe",
    unpack: `${clone}go build -o resetwatch.exe ./cmd/resetwatch\n.\\resetwatch.exe version`,
  },
  macos: {
    shell: "ZSH / BASH",
    help: "In Terminal, open the folder that should hold ResetWatch. Requires Git and Go 1.27.2 or newer.",
    executable: "./resetwatch",
    unpack: unixBuild,
  },
  linux: {
    shell: "BASH",
    help: "In your terminal, open the folder that should hold ResetWatch. Requires Git and Go 1.27.2 or newer.",
    executable: "./resetwatch",
    unpack: unixBuild,
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

function updatePlatformText(platform) {
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
  updatePlatformText(platform);
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
