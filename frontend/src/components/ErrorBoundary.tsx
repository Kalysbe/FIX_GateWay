import { Component, type ErrorInfo, type ReactNode } from "react";

type Props = { children: ReactNode };
type State = { error: Error | null };

export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("admin ui", error, info.componentStack);
  }

  render() {
    if (!this.state.error) return this.props.children;
    return (
      <div className="card m-6 space-y-3 border-rose-100 bg-rose-50">
        <h2 className="text-lg font-semibold text-danger">Ошибка интерфейса</h2>
        <p className="text-sm text-slate-600">{this.state.error.message}</p>
        <button type="button" className="btn-muted" onClick={() => this.setState({ error: null })}>
          Попробовать снова
        </button>
      </div>
    );
  }
}
