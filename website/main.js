'use strict';
// The landing-page example is local presentation state; it never calls Cooker.
const menuButton = document.querySelector('.menu-toggle');
const mobileMenu = document.querySelector('#mobile-nav');
function closeMenu(restoreFocus = false) {
  menuButton.setAttribute('aria-expanded', 'false');
  menuButton.setAttribute('aria-label', 'Open navigation');
  menuButton.querySelector('use').setAttribute('href', '#i-menu');
  mobileMenu.hidden = true;
  if (restoreFocus) menuButton.focus();
}
menuButton.addEventListener('click', () => {
  if (menuButton.getAttribute('aria-expanded') === 'true') return closeMenu();
  mobileMenu.hidden = false;
  menuButton.setAttribute('aria-expanded', 'true');
  menuButton.setAttribute('aria-label', 'Close navigation');
  menuButton.querySelector('use').setAttribute('href', '#i-close');
});
mobileMenu.addEventListener('click', (event) => {
  if (event.target.closest('a')) closeMenu();
});
document.addEventListener('keydown', (event) => {
  if (event.key === 'Escape' && !mobileMenu.hidden) closeMenu(true);
});
window.matchMedia('(min-width: 801px)').addEventListener('change', (event) => {
  if (event.matches) closeMenu();
});

const guideBase = 'https://github.com/santapong/Cooker/blob/develop/docs/';
const nodeData = {
  build: ['BUILD STAGE', 'Start with your container image.', 'Configure the Dockerfile, build context, and image output. A build is one visible stage in your pipeline.', 'guides/UAT.md'],
  test: ['TEST STAGE', 'Give checks their own place.', 'Run a command in a configured container runner. Inspect its logs and outcome before the next dependent stage begins.', 'guides/UAT.md'],
  push: ['PUSH STAGE', 'Make the image available.', 'Publish a built container image to your configured registry so the selected runtime can pull it.', 'guides/UAT.md'],
  review: ['APPROVAL STAGE', 'A deliberate pause before deploy.', 'Add a human checkpoint to your pipeline. The next stage waits until the required approvals are recorded.', 'guides/UAT.md'],
  deploy: ['DEPLOY STAGE', 'Carry the plan into your runtime.', 'The pipeline dispatches deployment work through the configured adapter. Stage status and logs help explain its outcome.', 'guides/UAT.md'],
  source: ['REVIEWED SOURCE', 'Know exactly which revision ships.', 'Inspect the repository, ordered Compose files, profiles, and explicit variables. The saved App stays pinned to a full Git commit.', 'guides/GITHUB-COMPOSE-DEPLOYMENT.md'],
  api: ['COMPOSE SERVICE', 'See how the API gets there.', 'The example API has its own Dockerfile. Cooker creates build, push, and deploy stages for the supported ECS mapping.', 'guides/GITHUB-COMPOSE-DEPLOYMENT.md'],
  worker: ['COMPOSE SERVICE', 'Keep each workload visible.', 'The worker is a separate executable service on the same compute target. Supported settings are checked before deployment.', 'guides/GITHUB-COMPOSE-DEPLOYMENT.md'],
  database: ['EXTERNAL BINDING', 'Keep the database where it belongs.', 'Reference connection keys from an Environment instead of launching the Compose database. You supply the existing database and network access.', 'reference/github-compose-architecture.md'],
};
let activeMode = 'pipeline';
const selection = { pipeline: 'review', compose: 'source' };
function selectNode(key) {
  const data = nodeData[key];
  if (!data) return;
  selection[activeMode] = key;
  document.querySelectorAll(`#panel-${activeMode} [data-node]`).forEach((button) => {
    const selected = button.dataset.node === key;
    button.setAttribute('aria-pressed', String(selected));
    button.classList.toggle('selected', selected);
  });
  document.querySelector('#detail-kind').textContent = data[0];
  document.querySelector('#detail-title').textContent = data[1];
  document.querySelector('#detail-description').textContent = data[2];
  document.querySelector('#detail-link').href = guideBase + data[3];
}
const tabs = [...document.querySelectorAll('[role="tab"]')];
function setMode(mode) {
  activeMode = mode;
  for (const tab of tabs) {
    const selected = tab.dataset.mode === mode;
    tab.setAttribute('aria-selected', String(selected));
    tab.tabIndex = selected ? 0 : -1;
    document.querySelector(`#panel-${tab.dataset.mode}`).hidden = !selected;
  }
  document.querySelector('#example-name').textContent = mode === 'pipeline' ? 'release-web' : 'shop-staging';
  selectNode(selection[mode]);
}
for (const tab of tabs) {
  tab.addEventListener('click', () => setMode(tab.dataset.mode));
  tab.addEventListener('keydown', (event) => {
    let index = tabs.indexOf(tab);
    if (event.key === 'ArrowRight') index = (index + 1) % tabs.length;
    else if (event.key === 'ArrowLeft') index = (index + tabs.length - 1) % tabs.length;
    else if (event.key === 'Home') index = 0;
    else if (event.key === 'End') index = tabs.length - 1;
    else return;
    event.preventDefault();
    setMode(tabs[index].dataset.mode);
    tabs[index].focus();
  });
}
document.querySelectorAll('[data-node]').forEach(button => {
  button.disabled = false;
  button.addEventListener('click', () => selectNode(button.dataset.node));
});
const copyButton = document.querySelector('#copy-command');
const copyStatus = document.querySelector('#copy-status');
copyButton.addEventListener('click', async () => {
  const code = document.querySelector('#setup-command');
  copyButton.disabled = true;
  try {
    if (!navigator.clipboard?.writeText) throw new Error('Clipboard unavailable');
    await navigator.clipboard.writeText(code.textContent);
    copyStatus.textContent = 'Commands copied. Paste them into your terminal when ready.';
    copyButton.querySelector('span').textContent = 'Copied';
    copyButton.querySelector('use').setAttribute('href', '#i-check');
  } catch {
    copyButton.querySelector('span').textContent = 'Copy';
    copyButton.querySelector('use').setAttribute('href', '#i-copy');
    const range = document.createRange();
    range.selectNodeContents(code);
    const selectedText = window.getSelection();
    selectedText.removeAllRanges();
    selectedText.addRange(range);
    code.parentElement.focus();
    copyStatus.textContent = 'Clipboard unavailable. Commands selected — use your browser’s Copy action.';
  } finally {
    copyButton.disabled = false;
  }
});
