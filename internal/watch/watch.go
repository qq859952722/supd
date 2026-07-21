// Package watch 实现 supd 文件发现与热重载。
// REQ-F-025~027: 文件发现规则、fsnotify 监听（500ms 防抖）、配置热重载
// REQ-C-012: 使用 fsnotify/fsnotify v1.10.1
package watch
