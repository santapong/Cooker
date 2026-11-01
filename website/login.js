'use strict';
// An isolated interaction prototype. Never transmits or persists form values.
const form = document.querySelector('#login-form');
const fields = document.querySelector('#login-fields');
const email = document.querySelector('#login-email');
const password = document.querySelector('#login-password');
const toggle = document.querySelector('#password-toggle');
const status = document.querySelector('#login-status');
const sso = document.querySelector('#sso-button');
const capsLock = document.querySelector('#caps-lock');
const layoutButtons = [...document.querySelectorAll('[data-layout-choice]')];

function setLayout(layout, updateAddress = true) {
  const selected = ['split', 'focused', 'paper', 'workshop', 'console'].includes(layout) ? layout : 'split';
  document.documentElement.dataset.layout = selected;
  for (const button of layoutButtons) {
    button.setAttribute('aria-pressed', String(button.dataset.layoutChoice === selected));
  }
  if (updateAddress) {
    const url = new URL(window.location.href);
    url.searchParams.set('layout', selected);
    history.replaceState(null, '', url);
  }
}
setLayout(new URLSearchParams(window.location.search).get('layout'), false);
for (const button of layoutButtons) {
  button.addEventListener('click', () => setLayout(button.dataset.layoutChoice));
  button.disabled = false;
}

function showStatus(message) {
  status.hidden = false;
  status.textContent = message;
}
function fieldError(input, message) {
  const output = document.querySelector(`#${input === email ? 'email' : 'password'}-error`);
  output.textContent = message;
  output.hidden = !message;
  input.setAttribute('aria-invalid', String(Boolean(message)));
}
form.addEventListener('submit', event => {
  event.preventDefault();
  status.hidden = true;
  const emailMessage = !email.value.trim() ? 'Enter your email address.' : email.validity.typeMismatch ? 'Enter a valid email address.' : '';
  const passwordMessage = !password.value ? 'Enter your password.' : '';
  fieldError(email, emailMessage);
  fieldError(password, passwordMessage);
  if (emailMessage || passwordMessage) {
    (emailMessage ? email : password).focus();
    return;
  }
  showStatus('This preview isn’t connected to a Cooker server. Your details stayed in this page; no sign-in request was made.');
});
for (const input of [email, password]) {
  input.addEventListener('input', () => {
    fieldError(input, '');
    status.hidden = true;
  });
}
toggle.addEventListener('click', () => {
  const visible = password.type === 'password';
  password.type = visible ? 'text' : 'password';
  toggle.setAttribute('aria-pressed', String(visible));
  toggle.setAttribute('aria-label', visible ? 'Hide password' : 'Show password');
  toggle.querySelector('use').setAttribute('href', visible ? '#login-eye-off' : '#login-eye');
});
for (const eventName of ['keydown', 'keyup']) {
  password.addEventListener(eventName, event => { capsLock.hidden = !event.getModifierState('CapsLock'); });
}
password.addEventListener('blur', () => { capsLock.hidden = true; });
sso.addEventListener('click', () => showStatus('Single sign-on uses the provider configured on your Cooker server. This design preview does not start authentication.'));
// Enable only after handlers exist, so a failed script cannot submit credentials.
fields.disabled = false;
sso.disabled = false;
