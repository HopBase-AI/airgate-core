import { Component, type ErrorInfo, type ReactNode } from 'react'
import { Button } from '@heroui/react'
import i18n from '../../i18n'
import { isChunkLoadError, tryReloadForStaleChunk } from '../../shared/chunkReload'

interface Props {
  children: ReactNode
  fallback?: ReactNode
}

interface State {
  hasError: boolean
  error: Error | null
  reloading: boolean
}

/**
 * 全局错误边界，捕获子组件树中的渲染错误，避免白屏
 */
export class ErrorBoundary extends Component<Props, State> {
  constructor(props: Props) {
    super(props)
    this.state = { hasError: false, error: null, reloading: false }
  }

  static getDerivedStateFromError(error: Error): Partial<State> {
    return { hasError: true, error }
  }

  componentDidCatch(error: Error, errorInfo: ErrorInfo) {
    console.error('[ErrorBoundary]', error, errorInfo)
    // 发版后旧 chunk 失效导致的动态加载失败：自动整页刷新一次换新版本，
    // 护栏期内二次失败才落到下方错误 UI。
    if (isChunkLoadError(error) && tryReloadForStaleChunk()) {
      this.setState({ reloading: true })
    }
  }

  render() {
    if (this.state.reloading) {
      // 整页刷新已触发，保持空白避免错误 UI 闪现
      return null
    }
    if (this.state.hasError) {
      if (this.props.fallback) {
        return this.props.fallback
      }
      return (
        <div style={{
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          justifyContent: 'center',
          minHeight: '50vh',
          padding: '2rem',
          fontFamily: 'system-ui, sans-serif',
        }}>
          <h2 style={{ marginBottom: '1rem', color: '#dc2626' }}>
            {i18n.t('error.page_error')}
          </h2>
          <p style={{ color: '#6b7280', marginBottom: '1.5rem' }}>
            {this.state.error?.message || i18n.t('error.unknown')}
          </p>
          <Button
            variant="secondary"
            onPress={() => {
              this.setState({ hasError: false, error: null })
              window.location.reload()
            }}
          >
            {i18n.t('error.refresh')}
          </Button>
        </div>
      )
    }

    return this.props.children
  }
}
