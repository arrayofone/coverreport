export function Button({ primary }: { primary?: boolean }) {
  const cls = primary ? "btn btn-primary" : "btn";
  return <button className={cls}>Click</button>;
}

export function IconButton() {
  return <Button primary />;
}
