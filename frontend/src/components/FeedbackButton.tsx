import { useEffect, useState } from 'react';
import { useTheme } from '../theme/ThemeProvider';
import { hexA } from '../theme/tokens';
import { Btn, Card } from './ui/atoms';
import { Icon } from './ui/Icon';
import { capabilitiesApi } from '../api/capabilities';
import { feedbackApi } from '../api/feedback';
import { ApiError } from '../api/client';
import { pushToast } from '../stores/toastStore';

const MAX_MESSAGE = 4000;

const CATEGORIES = ['bug', 'idea', 'other'] as const;
type Category = (typeof CATEGORIES)[number];

/**
 * FeedbackButton — floating round launcher (bottom-left) that opens a
 * small panel for filing feedback as a GitHub issue via POST /feedback.
 * Hidden when the server capability is off (no token configured); the
 * 503 toast below stays as a fallback in case the capability check and
 * the server posture ever disagree.
 */
export function FeedbackButton() {
  const t = useTheme();
  const [enabled, setEnabled] = useState(false);
  const [open, setOpen] = useState(false);
  const [category, setCategory] = useState<Category>('bug');
  const [message, setMessage] = useState('');
  const [pending, setPending] = useState(false);

  useEffect(() => {
    capabilitiesApi
      .get()
      .then((c) => setEnabled(c.feedback))
      .catch(() => setEnabled(false));
  }, []);

  if (!enabled) return null;

  const canSubmit = message.trim().length > 0 && !pending;

  const submit = async () => {
    if (!canSubmit) return;
    setPending(true);
    try {
      await feedbackApi.submit({
        category,
        message: message.trim(),
        pageUrl: window.location.href,
      });
      pushToast('success', 'Thanks — your feedback has been filed.');
      setMessage('');
      setCategory('bug');
      setOpen(false);
    } catch (e) {
      if (e instanceof ApiError && e.status === 503) {
        pushToast('error', "Feedback isn't configured on this server");
      } else if (e instanceof ApiError && e.status === 429) {
        pushToast('warning', 'Too many submissions — try again in a minute');
      } else {
        pushToast('error', 'Sending feedback failed — please try again.');
      }
    } finally {
      setPending(false);
    }
  };

  return (
    <>
      {open && (
        <Card
          pad={16}
          style={{
            position: 'fixed',
            left: 16,
            bottom: 72,
            zIndex: 9000,
            width: 340,
            maxWidth: 'calc(100vw - 32px)',
          }}
        >
          <div
            style={{
              fontFamily: t.mono,
              fontSize: 11,
              letterSpacing: 1.4,
              textTransform: 'uppercase',
              color: t.textMute,
              marginBottom: 12,
            }}
          >
            Send feedback
          </div>
          <div style={{ display: 'flex', gap: 6, marginBottom: 10 }}>
            {CATEGORIES.map((c) => (
              <Btn
                key={c}
                kind={category === c ? 'primary' : 'secondary'}
                onClick={() => setCategory(c)}
                style={{ flex: 1, textTransform: 'capitalize' }}
              >
                {c}
              </Btn>
            ))}
          </div>
          <textarea
            value={message}
            onChange={(e) => setMessage(e.target.value)}
            maxLength={MAX_MESSAGE}
            rows={5}
            placeholder="What's on your mind?"
            className="cc-field"
            style={{
              width: '100%',
              padding: '9px 11px',
              background: hexA(t.surfaceSolid, 0.5),
              color: t.text,
              border: `1px solid ${t.line}`,
              borderRadius: 8,
              fontSize: 13.5,
              fontFamily: t.sans,
              outline: 'none',
              resize: 'vertical',
              boxSizing: 'border-box',
              transition: 'border-color .15s, box-shadow .15s',
            }}
          />
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
              marginTop: 8,
            }}
          >
            <span style={{ fontFamily: t.mono, fontSize: 10.5, color: t.textMute }}>
              {message.length}/{MAX_MESSAGE}
            </span>
            <Btn kind="primary" onClick={submit} disabled={!canSubmit}>
              {pending ? 'Sending…' : 'Submit'}
            </Btn>
          </div>
        </Card>
      )}
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-label={open ? 'Close feedback' : 'Send feedback'}
        className="cc-btn"
        style={{
          position: 'fixed',
          left: 16,
          bottom: 16,
          zIndex: 9000,
          width: 44,
          height: 44,
          borderRadius: 999,
          display: 'grid',
          placeItems: 'center',
          cursor: 'pointer',
          background: `linear-gradient(135deg, ${t.violet}, ${t.violetGlow})`,
          color: '#fff',
          border: `1px solid ${hexA(t.violetGlow, 0.6)}`,
          boxShadow: `0 6px 18px ${hexA(t.violetGlow, 0.38)}`,
          transition: 'transform .15s, box-shadow .15s, filter .15s',
        }}
      >
        <Icon name={open ? 'close' : 'spark'} size={18} />
      </button>
    </>
  );
}
