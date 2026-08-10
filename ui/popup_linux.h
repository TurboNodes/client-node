#ifndef POPUP_LINUX_H
#define POPUP_LINUX_H

void popupInit(const char *html);
void popupToggle(void);
void popupShow(void);
void popupHide(void);
void popupSetState(const char *json);
void popupSetAnchor(double centerX, double topY, int valid);
void popupPrepare(void);
void popupRunLoop(void);
void popupQuitLoop(void);

#endif
