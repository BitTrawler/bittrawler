// Copy buttons. The interface works without JavaScript; this only adds the
// convenience of copying a value instead of selecting it by hand.
document.addEventListener('click', async (event) => {
  const button = event.target.closest('[data-copy]');
  if (!button) {
    return;
  }
  event.preventDefault();

  try {
    await navigator.clipboard.writeText(button.dataset.copy);
  } catch {
    return;
  }

  const label = button.textContent;
  button.textContent = 'Copied';
  button.classList.add('is-copied');
  window.setTimeout(() => {
    button.textContent = label;
    button.classList.remove('is-copied');
  }, 1400);
});
