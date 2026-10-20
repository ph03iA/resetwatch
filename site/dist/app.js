"use strict";

const clone = "git clone https://github.com/ph03iA/resetwatch.git\ncd resetwatch\ngo build ./cmd/resetwatch\n";

// The HTML shows the Windows commands, so the page works without JavaScript.
const platforms = {
  windows: { help: "Open PowerShell in the folder that should hold ResetWatch.", run: ".\\resetwatch.exe" },
  unix: { help: "Open Terminal in the folder that should hold ResetWatch.", run: "./resetwatch" },
};

const tabs = [...document.querySelectorAll("[data-os]")];
const status = document.getElementById("copy-status");

function selectPlatform(tab) {
  const { help, run } = platforms[tab.dataset.os];
  tabs.forEach((item) => {
    const selected = item === tab;
    item.setAttribute("aria-selected", String(selected));
    item.tabIndex = selected ? 0 : -1;
  });
  document.getElementById("install-panel").setAttribute("aria-labelledby", tab.id);
  document.getElementById("build-help").textContent = help;
  document.getElementById("build-command").textContent = `${clone}${run} version`;
  document.getElementById("setup-command").textContent = `${run} setup`;
  document.getElementById("watch-command").textContent = `${run} watch`;
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

function selectText(target) {
  const selection = window.getSelection();
  if (!selection) return;
  const range = document.createRange();
  range.selectNodeContents(target);
  selection.removeAllRanges();
  selection.addRange(range);
}

async function copyCommand(event) {
  const button = event.currentTarget;
  const target = document.getElementById(button.dataset.copy);
  try {
    await navigator.clipboard.writeText(target.textContent);
    button.textContent = "Copied";
    button.dataset.copied = "true";
    delete status.dataset.error;
    status.textContent = "Copied to your clipboard."; // read aloud by screen readers; hidden on screen
    window.setTimeout(() => {
      button.textContent = "Copy";
      delete button.dataset.copied;
      status.textContent = "";
    }, 2000);
  } catch {
    selectText(target);
    status.dataset.error = "true";
    status.textContent = "Clipboard access is unavailable. The commands are selected; copy them with your keyboard.";
  }
}

// The example email is a separate page; fit the frame to it so it never scrolls inside.
function fitEmail(frame) {
  try {
    frame.style.height = `${frame.contentDocument.body.scrollHeight + 2}px`;
  } catch {
    // Keep the stylesheet's fixed height.
  }
}

tabs.forEach((tab) => {
  tab.addEventListener("click", () => selectPlatform(tab));
  tab.addEventListener("keydown", handleTabKey);
});
document.querySelectorAll("[data-copy]").forEach((button) => button.addEventListener("click", copyCommand));

const email = document.querySelector(".specimen iframe");
if (email) {
  email.addEventListener("load", () => fitEmail(email));
  window.addEventListener("resize", () => fitEmail(email));
  if (email.contentDocument?.readyState === "complete") fitEmail(email);
}
