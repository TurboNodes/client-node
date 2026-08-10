#include <gtk/gtk.h>
#include <webkit2/webkit2.h>
#include <string.h>
#include "popup_linux.h"
#include "_cgo_export.h"

#define POPUP_WIDTH 300
#define POPUP_HEIGHT 400
#define SCREEN_MARGIN 8

static GtkWidget *popupWindow = NULL;
static WebKitWebView *popupWebView = NULL;

// Every GTK call has to happen on the thread running gtk_main. Go calls in
// from arbitrary goroutines, so each entry point below hands a small payload
// to g_idle_add and lets the GTK loop run it.

static gboolean idle_init(gpointer data);
static gboolean idle_toggle(gpointer data);
static gboolean idle_show(gpointer data);
static gboolean idle_hide(gpointer data);
static gboolean idle_set_state(gpointer data);
static void hide_popup_now(void);

// Where to hang the popup from, in screen pixels. Linux cannot report the tray
// icon's position (see systray.IconRect), so this stays invalid there and the
// pointer is used instead; the plumbing exists so every platform is placed the
// same way if a desktop ever does report it.
static double anchorCenterX = 0;
static double anchorTopY = 0;
static gboolean anchorValid = FALSE;

// popupHadFocus gates click-away dismissal; see the focus handlers below.
static gboolean popupHadFocus = FALSE;

static gboolean on_focus_in(GtkWidget *widget, GdkEventFocus *event, gpointer user_data) {
  popupHadFocus = TRUE;
  return FALSE;
}

static gboolean on_focus_out(GtkWidget *widget, GdkEventFocus *event, gpointer user_data) {
  if (!popupHadFocus) {
    return FALSE;
  }
  popupHadFocus = FALSE;
  hide_popup_now();
  return FALSE;
}

// script_message receives window.webkit.messageHandlers.turbo.postMessage(...)
static void script_message(WebKitUserContentManager *manager,
                           WebKitJavascriptResult *result,
                           gpointer user_data) {
  JSCValue *value = webkit_javascript_result_get_js_value(result);
  if (!jsc_value_is_string(value)) {
    return;
  }
  char *action = jsc_value_to_string(value);
  PopupOnAction(action);
  g_free(action);
}

void popupInit(const char *html) {
  // Copy before handing to the idle callback: the caller frees this as soon
  // as popupInit returns, well before the GTK loop gets to it.
  g_idle_add(idle_init, g_strdup(html));
}

static gboolean idle_init(gpointer data) {
  char *html = (char *)data;

  popupWindow = gtk_window_new(GTK_WINDOW_POPUP);
  gtk_window_set_default_size(GTK_WINDOW(popupWindow), POPUP_WIDTH, POPUP_HEIGHT);
  gtk_window_set_resizable(GTK_WINDOW(popupWindow), FALSE);
  gtk_window_set_decorated(GTK_WINDOW(popupWindow), FALSE);
  gtk_window_set_skip_taskbar_hint(GTK_WINDOW(popupWindow), TRUE);
  gtk_window_set_skip_pager_hint(GTK_WINDOW(popupWindow), TRUE);
  gtk_window_set_keep_above(GTK_WINDOW(popupWindow), TRUE);

  // Let the page's own rounded card show through instead of a grey GTK slab.
  GdkScreen *screen = gtk_widget_get_screen(popupWindow);
  GdkVisual *rgba = gdk_screen_get_rgba_visual(screen);
  if (rgba != NULL) {
    gtk_widget_set_visual(popupWindow, rgba);
  }
  gtk_widget_set_app_paintable(popupWindow, TRUE);

  WebKitUserContentManager *manager = webkit_user_content_manager_new();
  webkit_user_content_manager_register_script_message_handler(manager, "turbo");
  g_signal_connect(manager, "script-message-received::turbo",
                   G_CALLBACK(script_message), NULL);

  popupWebView = WEBKIT_WEB_VIEW(webkit_web_view_new_with_user_content_manager(manager));

  GdkRGBA transparent = {0, 0, 0, 0};
  webkit_web_view_set_background_color(popupWebView, &transparent);

  webkit_web_view_load_html(popupWebView, html, NULL);
  gtk_container_add(GTK_CONTAINER(popupWindow), GTK_WIDGET(popupWebView));

  // A popup window has no close button; hide instead of being destroyed so the
  // same window (and its loaded page) can be shown again.
  g_signal_connect(popupWindow, "delete-event", G_CALLBACK(gtk_widget_hide_on_delete), NULL);

  // Click-away dismissal. This deliberately waits for the window to have held
  // focus first: mapping a window emits a focus-out before anything grants it
  // focus, so dismissing on the first focus-out would hide the popup
  // immediately after every show.
  g_signal_connect(popupWindow, "focus-in-event", G_CALLBACK(on_focus_in), NULL);
  g_signal_connect(popupWindow, "focus-out-event", G_CALLBACK(on_focus_out), NULL);

  g_free(html);
  return G_SOURCE_REMOVE;
}

// position_popup anchors the popup to the pointer, which is over the tray icon
// at click time, then clamps it into the monitor's work area. Nothing in the
// StatusNotifierItem protocol reports where the desktop actually drew the icon,
// so the pointer is the only anchor available.
static void position_popup(void) {
  GdkDisplay *display = gdk_display_get_default();
  if (display == NULL) {
    return;
  }

  int px = 0, py = 0;
  if (anchorValid) {
    px = (int)anchorCenterX;
    py = (int)anchorTopY;
  } else {
    GdkSeat *seat = gdk_display_get_default_seat(display);
    GdkDevice *pointer = gdk_seat_get_pointer(seat);
    gdk_device_get_position(pointer, NULL, &px, &py);
  }

  GdkMonitor *monitor = gdk_display_get_monitor_at_point(display, px, py);
  GdkRectangle area;
  if (monitor != NULL) {
    gdk_monitor_get_workarea(monitor, &area);
  } else {
    area.x = 0; area.y = 0; area.width = 1920; area.height = 1080;
  }

  int x = px - (POPUP_WIDTH / 2);
  if (x < area.x + SCREEN_MARGIN) {
    x = area.x + SCREEN_MARGIN;
  }
  if (x + POPUP_WIDTH > area.x + area.width - SCREEN_MARGIN) {
    x = area.x + area.width - SCREEN_MARGIN - POPUP_WIDTH;
  }

  // Panels live at the top or the bottom depending on the desktop. Drop the
  // popup below the pointer when there is room, otherwise flip it above.
  int y = py + SCREEN_MARGIN;
  if (y + POPUP_HEIGHT > area.y + area.height - SCREEN_MARGIN) {
    y = py - POPUP_HEIGHT - SCREEN_MARGIN;
  }
  if (y < area.y + SCREEN_MARGIN) {
    y = area.y + SCREEN_MARGIN;
  }

  gtk_window_move(GTK_WINDOW(popupWindow), x, y);
}

// hide_popup_now tells the page it is going away first, so a leftover text
// selection is not still highlighted the next time the popup opens.
static void hide_popup_now(void) {
  if (popupWebView != NULL) {
    webkit_web_view_evaluate_javascript(popupWebView,
        "window.turbo && window.turbo.onHidden();", -1, NULL, NULL, NULL, NULL, NULL);
  }
  gtk_widget_hide(popupWindow);
}

static void show_popup_now(void) {
  popupHadFocus = FALSE;
  position_popup();
  gtk_widget_show_all(popupWindow);
  gtk_window_present(GTK_WINDOW(popupWindow));
}

void popupToggle(void) { g_idle_add(idle_toggle, NULL); }
void popupShow(void) { g_idle_add(idle_show, NULL); }
void popupHide(void) { g_idle_add(idle_hide, NULL); }

static gboolean idle_toggle(gpointer data) {
  if (popupWindow == NULL) {
    return G_SOURCE_REMOVE;
  }
  if (gtk_widget_get_visible(popupWindow)) {
    hide_popup_now();
  } else {
    show_popup_now();
  }
  return G_SOURCE_REMOVE;
}

static gboolean idle_show(gpointer data) {
  if (popupWindow != NULL && !gtk_widget_get_visible(popupWindow)) {
    show_popup_now();
  }
  return G_SOURCE_REMOVE;
}

static gboolean idle_hide(gpointer data) {
  if (popupWindow != NULL) {
    hide_popup_now();
  }
  return G_SOURCE_REMOVE;
}

void popupSetAnchor(double centerX, double topY, int valid) {
  anchorCenterX = centerX;
  anchorTopY = topY;
  anchorValid = valid ? TRUE : FALSE;
}

void popupSetState(const char *json) {
  g_idle_add(idle_set_state, g_strdup(json));
}

static gboolean idle_set_state(gpointer data) {
  char *json = (char *)data;
  if (popupWebView != NULL) {
    char *js = g_strdup_printf("window.turbo && window.turbo.setState(%s);", json);
    // Errors before the page has loaded are expected; the page asks for state
    // itself once it is ready.
    webkit_web_view_evaluate_javascript(popupWebView, js, -1, NULL, NULL, NULL, NULL, NULL);
    g_free(js);
  }
  g_free(json);
  return G_SOURCE_REMOVE;
}

void popupPrepare(void) {
  gtk_init(NULL, NULL);
}

void popupRunLoop(void) {
  gtk_main();
}

void popupQuitLoop(void) {
  gtk_main_quit();
}
