interface Props {
  /** Vertical separator inside a flex row. */
  vertical?: boolean;
  className?: string;
}

/** 1px `--line` separator. Prefer this over borders and boxes. */
export default function Hairline({ vertical = false, className }: Props) {
  const cls = ['hairline', vertical ? 'hairline-v' : '', className ?? ''].filter(Boolean).join(' ');
  if (vertical) return <div role="separator" aria-orientation="vertical" className={cls} />;
  return <hr className={cls} />;
}
