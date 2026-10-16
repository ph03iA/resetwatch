"use strict";

const clone = "git clone https://github.com/ph03iA/resetwatch.git\ncd resetwatch\ngo build ./cmd/resetwatch\n";

// Windows commands also work in Command Prompt: .\ paths and double quotes only.
const platforms = {
  windows: {
    shell: "PowerShell / CMD",
    help: "In PowerShell or Command Prompt, open the folder that should hold ResetWatch. Requires Git and Go 1.27.2 or newer.",
    executable: ".\\resetwatch.exe",
  },
  unix: {
    shell: "Terminal",
    help: "In Terminal, open the folder that should hold ResetWatch. Requires Git and Go 1.27.2 or newer.",
    executable: "./resetwatch",
  },
};

const tabs = [...document.querySelectorAll("[data-os]")];
const status = document.getElementById("copy-status");

function selectPlatform(tab) {
  const platform = platforms[tab.dataset.os];
  const run = platform.executable;
  tabs.forEach((item) => {
    const selected = item === tab;
    item.setAttribute("aria-selected", String(selected));
    item.tabIndex = selected ? 0 : -1;
  });
  document.getElementById("installation-panel").setAttribute("aria-labelledby", tab.id);
  document.getElementById("shell-label").textContent = platform.shell;
  document.getElementById("unpack-help").textContent = platform.help;
  document.getElementById("install-command").textContent = `${clone}${run} version`;
  document.getElementById("connect-command").textContent = `${run} connect codex`;
  document.getElementById("watch-command").textContent = `${run} notify --email you@example.com --from "ResetWatch <onboarding@resend.dev>"\n${run} watch`;
}

function handleTabKey(event) {
  const index = tabs.indexOf(event.currentTarget);
  const offsets = { ArrowRight: 1, ArrowLeft: -1, Home: -index, End: tabs.length - 1 - index };
  const offset = offsets[event.key];
  if (offset === undefined) return;
  event.preventDefault();
  const next = tabs[(index + offset + tabs.length) % tabs.length];
  selectPlatform(next);
  next.focus();
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
  // The button already shows "Copied"; the zero-height status is for screen readers.
  delete status.dataset.error;
  status.textContent = "Commands copied to your clipboard.";
  window.setTimeout(() => {
    button.firstChild.textContent = "Copy ";
    delete button.dataset.copied;
    status.textContent = "";
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
    status.dataset.error = "true";
    status.textContent = "Clipboard access is unavailable. The commands are selected; use your keyboard to copy.";
  }
}

tabs.forEach((tab) => {
  tab.addEventListener("click", () => selectPlatform(tab));
  tab.addEventListener("keydown", handleTabKey);
});
document.querySelectorAll("[data-copy]").forEach((button) => button.addEventListener("click", copyCommand));

// Content stays visible without JavaScript; sections only fade in when the observer exists.
if ("IntersectionObserver" in window) {
  const observer = new IntersectionObserver((entries) => {
    entries.filter((entry) => entry.isIntersecting).forEach((entry) => {
      entry.target.classList.add("is-visible");
      observer.unobserve(entry.target);
    });
  }, { rootMargin: "0px 0px -8% 0px" });
  document.documentElement.classList.add("js-reveal");
  document.querySelectorAll("main .section-heading, .lifecycle li, .install-workbook, .notes-layout > *, .footer-heading").forEach((element) => {
    element.classList.add("reveal");
    observer.observe(element);
  });
}
