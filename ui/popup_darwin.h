#ifndef POPUP_DARWIN_H
#define POPUP_DARWIN_H

void popupInit(const char *html);
void popupToggle(void);
void popupShow(void);
void popupHide(void);
void popupSetState(const char *json);
void popupSetAnchor(double centerX, double bottomY, int valid);

#endif
