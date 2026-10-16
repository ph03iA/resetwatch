"use strict";

const status = document.getElementById("copy-status");

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
